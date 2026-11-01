package auth

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"
)

func TestPublisherTokenFreshnessScopesAndSecretHash(t *testing.T) {
	s := fixture(t)
	_, session := loggedIn(t, s)
	ctx := context.Background()
	in := TokenInput{Label: "Runner", Scopes: []string{"draft", "publish"}, AllowArticles: true, Days: 90}
	out, e := s.CreatePublisherToken(ctx, session, in)
	if e != nil {
		t.Fatal(e)
	}
	if !validSecret(out.Secret) {
		t.Fatal("missing one-time secret")
	}
	var stored string
	if e = s.DB.QueryRow("SELECT token_hash FROM api_tokens WHERE id=?", out.ID).Scan(&stored); e != nil || stored == out.Secret || stored != digest(out.Secret) {
		t.Fatal("raw credential stored", e)
	}
	r := httptest.NewRequest("GET", "https://example.test/api/v1/publisher/content/a/en", nil)
	r.Header.Set("Authorization", "Bearer "+out.Secret)
	token, e := s.PublisherRequest(r)
	if e != nil {
		t.Fatal(e)
	}
	tx, _ := s.DB.BeginTx(ctx, nil)
	if e = s.PublisherCheck(ctx, tx, token, "draft", "a", "", "article", false); e != nil {
		t.Fatal(e)
	}
	if e = s.PublisherCheck(ctx, tx, token, "draft", "a", "", "article", true); e == nil {
		t.Fatal("quiz escalation")
	}
	if e = s.PublisherCheck(ctx, tx, token, "draft", "a", "elsewhere", "lesson", false); e == nil {
		t.Fatal("collection escalation")
	}
	tx.Rollback()
	s.DB.Exec("UPDATE api_tokens SET revoked_at=? WHERE id=?", s.now(), out.ID)
	if _, e = s.PublisherRequest(r); e == nil {
		t.Fatal("revoked token accepted")
	}
	old := s.Now()
	s.Now = func() time.Time { return old.Add(31 * time.Minute) }
	if _, e = s.CreatePublisherToken(ctx, session, in); e == nil {
		t.Fatal("stale owner created token")
	}
	s.Now = func() time.Time { return old }
	in.Scopes = []string{"admin"}
	if _, e = s.CreatePublisherToken(ctx, session, in); e == nil {
		t.Fatal("unknown capability")
	}
}
