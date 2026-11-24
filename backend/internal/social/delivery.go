package social

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"time"

	"github.com/mbougarne/modawana/backend/internal/outbox"
)

type queuedPost struct {
	id, connection, provider, instance, key, locale, revision, path, body, outcome, image, container, title string
	first                                                                                                   int64
}

func (s *Service) Prepare(ctx context.Context, job outbox.Delivery) (outbox.Effect, error) {
	if job.PluginID != "" || job.Event.Name != outbox.SocialScheduled || job.Event.Version != 1 || job.Event.SourceID != job.ID {
		return nil, outbox.ErrRejected
	}
	post, connection, reason, err := s.check(ctx, job.ID, false)
	if err != nil {
		return nil, err
	}
	if post.outcome != "" {
		return nil, nil
	}
	if reason != "" {
		return stopped(post, reason, false), nil
	}
	box, err := s.box()
	if err != nil {
		return stopped(post, "credentials_unavailable", false), nil
	}
	raw, err := box.Open(nil, nil, connection.token, connectionAAD(connection))
	if err != nil {
		return stopped(post, "credentials_unavailable", false), nil
	}
	token := string(raw)
	provider := s.providers[post.provider]
	if refresher, ok := provider.(credentialRefresher); ok {
		updated, err := refresher.Refresh(ctx, token)
		if err != nil {
			return s.providerFailure(provider, post, err, false)
		}
		if updated != token {
			encrypted := box.Seal(nil, nil, []byte(updated), connectionAAD(connection))
			result, err := s.DB.ExecContext(ctx, "UPDATE social_connections SET token=? WHERE id=? AND token=?", encrypted, connection.ID, connection.token)
			if err != nil {
				return nil, err
			}
			n, err := result.RowsAffected()
			if err != nil {
				return nil, err
			}
			if n != 1 {
				return stopped(post, "connection_changed", false), nil
			}
			token = updated
		}
	}
	account, err := provider.Verify(ctx, token)
	if err != nil {
		return s.providerFailure(provider, post, err, false)
	}
	if account.ID != connection.accountID {
		return stopped(post, "destination_changed", true), nil
	}
	message := Message{AccountID: account.ID, OperationID: post.id, Text: post.body, Title: post.title,
		Link: s.baseURL + post.path, Image: post.image, Container: post.container}
	if media, ok := provider.(mediaPublisher); ok {
		if message.Container == "" {
			container, err := media.PrepareMedia(ctx, token, message)
			if err != nil {
				return s.providerFailure(provider, post, err, false)
			}
			if _, err = s.DB.ExecContext(ctx, "UPDATE social_posts SET container=? WHERE id=? AND first_dispatch_at=0 AND outcome=''", container, post.id); err != nil {
				return nil, err
			}
			message.Container = container
		}
		if err := media.MediaReady(ctx, token, message.Container); err != nil {
			return s.providerFailure(provider, post, err, false)
		}
	}
	// Check the publication, destination and cancellation once again after the
	// provider query, and commit the dispatch marker before sending.
	post, _, reason, err = s.check(ctx, job.ID, true)
	if err != nil {
		return nil, err
	}
	if post.outcome != "" {
		return nil, nil
	}
	if reason != "" {
		return stopped(post, reason, false), nil
	}
	receipt, err := provider.Send(ctx, token, message)
	if err != nil {
		var failure *providerError
		if reconciler, ok := provider.(interface {
			Reconcile(context.Context, string, Message) bool
		}); ok &&
			errors.As(err, &failure) && (failure.status == 0 || failure.status >= 500) && reconciler.Reconcile(ctx, token, message) {
			return finish(post, "sent", "", Receipt{}, false), nil
		}
		return s.providerFailure(provider, post, err, true)
	}
	if receipt.Account.ID != connection.accountID {
		return finish(post, "uncertain", "unexpected_receipt", Receipt{}, true), nil
	}
	return finish(post, "sent", "", receipt, false), nil
}

func (s *Service) check(ctx context.Context, id string, dispatch bool) (queuedPost, Connection, string, error) {
	var p queuedPost
	var c Connection
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return p, c, "", err
	}
	defer tx.Rollback()
	err = tx.QueryRowContext(ctx, `SELECT id,connection_id,provider,instance,content_key,locale,revision_id,path,body,outcome,first_dispatch_at,image,container,
		coalesce((SELECT title FROM revisions WHERE id=social_posts.revision_id),'')
		FROM social_posts WHERE id=?`, id).Scan(&p.id, &p.connection, &p.provider, &p.instance, &p.key, &p.locale, &p.revision, &p.path, &p.body, &p.outcome, &p.first, &p.image, &p.container, &p.title)
	if err != nil || p.outcome != "" {
		return p, c, "", err
	}
	provider := s.providers[p.provider]
	if provider == nil || p.instance != provider.Instance() {
		return p, c, "connection_changed", nil
	}
	c, err = readConnection(ctx, tx, "id", p.connection)
	if errors.Is(err, sql.ErrNoRows) || err == nil && (c.Provider != p.provider || c.Instance != p.instance) {
		return p, c, "connection_changed", nil
	}
	if err != nil {
		return p, c, "", err
	}
	now := time.Now()
	if p.first > 0 && provider.ReplayWindow() == 0 {
		return p, c, "delivery_unconfirmed", nil
	}
	if p.first > 0 && !now.Before(time.UnixMilli(p.first).Add(provider.ReplayWindow())) {
		return p, c, "replay_window_expired", nil
	}
	path, err := s.Content.SocialRevision(ctx, tx, p.key, p.locale, p.revision)
	if errors.Is(err, sql.ErrNoRows) || err == nil && (path != p.path || !s.matchesOrigin(p)) {
		return p, c, "publication_changed", nil
	}
	if err != nil {
		return p, c, "", err
	}
	if p.image != "" {
		cover, err := s.Content.SocialCover(ctx, tx, p.revision)
		if errors.Is(err, sql.ErrNoRows) || err == nil && s.baseURL+cover != p.image {
			return p, c, "publication_changed", nil
		}
		if err != nil {
			return p, c, "", err
		}
	}
	if dispatch && p.first == 0 {
		if _, err := tx.ExecContext(ctx, "UPDATE social_posts SET first_dispatch_at=? WHERE id=?", now.UnixMilli(), p.id); err != nil {
			return p, c, "", err
		}
		// The returned value records whether a previous send is possible. A
		// definitive rejection of the first send is still a known failure.
	}
	return p, c, "", tx.Commit()
}

func (s *Service) matchesOrigin(p queuedPost) bool {
	suffix := "\n\n" + s.baseURL + p.path
	return len(p.body) > len(suffix) && p.body[len(p.body)-len(suffix):] == suffix
}

func (s *Service) providerFailure(provider Provider, p queuedPost, err error, send bool) (outbox.Effect, error) {
	var failure *providerError
	if !errors.As(err, &failure) {
		return stopped(p, "provider_unavailable", false), nil
	}
	switch failure.status {
	case http.StatusUnauthorized, http.StatusForbidden:
		return stopped(p, "access_revoked", true), nil
	}
	if send && provider.ReplayWindow() == 0 && (failure.status == 0 || failure.status >= 500) {
		return finish(p, "uncertain", "delivery_unconfirmed", Receipt{}, false), nil
	}
	if send && provider.ReplayWindow() == 0 && failure.status == http.StatusTooManyRequests {
		return finish(p, "failed", "provider_retry", Receipt{}, false), nil
	}
	if failure.status == 0 || failure.status == http.StatusTooManyRequests || failure.status >= 500 {
		deadline := time.Now().Add(provider.ReplayWindow())
		if p.first > 0 {
			deadline = time.UnixMilli(p.first).Add(provider.ReplayWindow())
		}
		if (send || p.first > 0) && !failure.retry.Before(deadline) {
			return finish(p, "uncertain", "replay_window_expired", Receipt{}, false), nil
		}
		if failure.retry.After(time.Now().Add(24 * time.Hour)) {
			return stopped(p, "provider_unavailable", false), nil
		}
		// Retry the exact delivery ID and body. A previous POST may have
		// succeeded even though this attempt did not receive its response.
		return nil, &outbox.ProviderRetry{At: failure.retry}
	}
	if p.first > 0 {
		return finish(p, "uncertain", "provider_rejected", Receipt{}, false), nil
	}
	return finish(p, "failed", "provider_rejected", Receipt{}, false), nil
}

func stopped(p queuedPost, reason string, disconnect bool) outbox.Effect {
	if p.first > 0 {
		return finish(p, "uncertain", reason, Receipt{}, disconnect)
	}
	return finish(p, "blocked", reason, Receipt{}, disconnect)
}

func finish(p queuedPost, status, reason string, receipt Receipt, disconnect bool) outbox.Effect {
	return func(ctx context.Context, tx *sql.Tx) error {
		if disconnect {
			if _, err := tx.ExecContext(ctx, "DELETE FROM social_connections WHERE id=?", p.connection); err != nil {
				return err
			}
		}
		_, err := tx.ExecContext(ctx, `UPDATE social_posts SET outcome=?,reason=?,remote_id=?,remote_url=? WHERE id=? AND outcome=''`,
			status, reason, receipt.ID, receipt.URL, p.id)
		return err
	}
}
