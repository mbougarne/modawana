package content

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/mbougarne/modawana/backend/internal/db"
	"github.com/mbougarne/modawana/backend/internal/outbox"
)

func TestPublicationOutboxCommitsExactRevisionAndRollsBack(t *testing.T) {
	s, owner := fixture(t)
	ctx := t.Context()
	const key = "event-article"
	saved := saveBulkArticle(t, s, owner, key)
	other := saveBulkArticle(t, s, owner, "other-event-article")
	a := Action{ExpectedVersion: &saved.Version, RevisionID: saved.RevisionID}
	countEvents := func(want int) {
		t.Helper()
		var n int
		if err := s.DB.QueryRow("SELECT count(*) FROM outbox_events").Scan(&n); err != nil || n != want {
			t.Fatal("event count", n, err)
		}
	}
	if _, err := s.ChangePublicationBulk(ctx, owner, BulkPublication{Operation: "publish", Items: []BulkPublicationItem{
		{Key: key, Locale: "en", ExpectedVersion: &saved.Version, RevisionID: saved.RevisionID},
		{Key: "other-event-article", Locale: "en", ExpectedVersion: number(99), RevisionID: other.RevisionID},
	}}); !errors.Is(err, db.ErrVersionConflict) {
		t.Fatal(err)
	}
	countEvents(0)
	if _, err := s.DB.Exec(`CREATE TRIGGER reject_event BEFORE INSERT ON outbox_events
		BEGIN SELECT RAISE(ABORT,'enqueue rejected'); END`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ChangePublication(ctx, owner, key, "en", "publish-event", "publish", a); err == nil {
		t.Fatal("publication committed without its event")
	}
	draft, err := s.Draft(ctx, key, "en")
	if err != nil || draft.PublishedID != "" || draft.Version != saved.Version {
		t.Fatal("publication was not rolled back", draft, err)
	}
	if _, err := s.DB.Exec("DROP TRIGGER reject_event"); err != nil {
		t.Fatal(err)
	}
	published, err := s.ChangePublication(ctx, owner, key, "en", "publish-event", "publish", a)
	if err != nil {
		t.Fatal(err)
	}
	replay, err := s.ChangePublication(ctx, owner, key, "en", "publish-event", "publish", a)
	if err != nil || replay != published {
		t.Fatal("idempotency changed", replay, err)
	}
	countEvents(1)
	var raw, topic string
	var contract int
	var version int64
	if err := s.DB.QueryRow("SELECT payload,name,contract_version,source_version FROM outbox_events").
		Scan(&raw, &topic, &contract, &version); err != nil {
		t.Fatal(err)
	}
	var event outbox.Publication
	if err := json.Unmarshal([]byte(raw), &event); err != nil || event.RevisionID != saved.RevisionID ||
		event.Locale != "en" || event.ContentID == "" || topic != outbox.ContentPublished ||
		contract != outbox.ContentPublishedVersion || version != published.Version {
		t.Fatal("unexpected publication event", event, topic, contract, version, err)
	}
}
