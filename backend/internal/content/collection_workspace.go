package content

import (
	"context"

	"github.com/mbougarne/modawana/backend/internal/access"
)

type WorkspaceMember struct {
	Key             string            `json:"key"`
	Announcements   map[string]string `json:"announcements"`
	ExpectedVersion *int64            `json:"expected_version,omitempty"`
}
type CollectionWorkspaceInput struct {
	CollectionInput
	Members []WorkspaceMember `json:"members"`
}

// SaveCollectionWorkspace commits metadata, guarded conversions, order and activation together.
// It never publishes a draft revision or changes another collection's membership.
func (s *Service) SaveCollectionWorkspace(ctx context.Context, owner, key, locale, retryKey string, in CollectionWorkspaceInput) (CollectionResult, error) {
	var out CollectionResult
	if len(in.Members) > 500 || access.Actor(ctx, owner) != owner {
		return out, ErrInvalid
	}
	request := hash(struct {
		Key, Locale string
		Input       CollectionWorkspaceInput
	}{key, locale, in})
	err := s.Files.Exclusive(ctx, func() error {
		tx, err := s.DB.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		defer tx.Rollback()
		if err = access.CheckMutation(ctx, tx, ""); err != nil {
			return err
		}
		prior, ok, err := retry(ctx, tx, owner, "collection-workspace", retryKey, request)
		if err != nil {
			return err
		}
		if ok {
			if err = tx.QueryRowContext(ctx, "SELECT id FROM collections WHERE key=?", key).Scan(&out.ID); err != nil {
				return err
			}
			out.PlanID = prior.RevisionID
			out.Version = prior.Version
			return nil
		}
		out, err = s.saveCollectionIn(ctx, tx, key, locale, in.CollectionInput)
		if err != nil {
			return err
		}
		members := make([]PlanMember, 0, len(in.Members))
		seen := map[string]bool{}
		target := "lesson"
		if in.Kind == "docs" {
			target = "doc"
		}
		for _, member := range in.Members {
			if !keyPattern.MatchString(member.Key) || seen[member.Key] {
				return ErrInvalid
			}
			seen[member.Key] = true
			var kind string
			if err = tx.QueryRowContext(ctx, "SELECT kind FROM content_items WHERE key=?", member.Key).Scan(&kind); err != nil {
				return typeIssue("members", "collection_member_locale")
			}
			if kind == "article" {
				var version int64
				if err = tx.QueryRowContext(ctx, "SELECT version FROM collections WHERE id=?", out.ID).Scan(&version); err != nil {
					return err
				}
				_, err = s.changeTypeIn(ctx, tx, owner, member.Key, locale, TypeChange{ExpectedVersion: member.ExpectedVersion, FromKind: "article", Kind: target, CollectionKey: key, ExpectedCollectionVersion: &version}, false)
				if err != nil {
					return err
				}
			} else if kind != target {
				return typeIssue("members", "conversion_kind")
			}
			members = append(members, PlanMember{Key: member.Key, Announcements: member.Announcements})
		}
		var version int64
		if err = tx.QueryRowContext(ctx, "SELECT version FROM collections WHERE id=?", out.ID).Scan(&version); err != nil {
			return err
		}
		saved, err := s.savePlanIn(ctx, tx, owner, key, PlanInput{ExpectedVersion: &version, Members: members})
		if err != nil {
			return err
		}
		out, err = s.applyPlanIn(ctx, tx, key, ApplyPlanInput{ExpectedVersion: &saved.Version, PlanID: saved.PlanID})
		if err != nil {
			return err
		}
		out.PlanID = saved.PlanID
		if err = remember(ctx, tx, owner, "collection-workspace", retryKey, request, Result{RevisionID: out.PlanID, Version: out.Version}); err != nil {
			return err
		}
		return tx.Commit()
	})
	return out, err
}
