package store

import (
	"errors"
	"os"
	"sync"
	"testing"

	"github.com/mbougarne/modawana/backend/internal/db"
)

func storeFixture(t *testing.T) *Service {
	t.Helper()
	dir := t.TempDir()
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	d, err := db.Open(t.Context(), dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	return &Service{DB: d}
}

func TestStockConcurrentAdjustmentsAndReplay(t *testing.T) {
	s := storeFixture(t)
	ctx := t.Context()
	settings, err := s.SaveSettings(ctx, SettingsInput{Version: 1, Enabled: true, Currency: "JPY"})
	if err != nil || settings.Digits != 0 {
		t.Fatal(settings, err)
	}
	for _, key := range []string{"first", "second"} {
		if _, err = s.SaveDraft(ctx, key, "en", DraftInput{Slug: key, Title: key, Description: "Description"}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = s.Publication(ctx, "first", "en", 1, true); !errors.Is(err, ErrVariant) {
		t.Fatal("published without an active SKU", err)
	}
	variant, err := s.SaveVariant(ctx, "first", " red-1 ", VariantInput{PriceMinor: 3000, Active: true})
	if err != nil || variant.SKU != "RED-1" || variant.Stock != 0 {
		t.Fatal(variant, err)
	}
	if _, err = s.SaveVariant(ctx, "second", "red-1", VariantInput{PriceMinor: 3000, Active: true}); !errors.Is(err, ErrConflict) {
		t.Fatal("reused SKU across products", err)
	}
	if _, err = s.SaveSettings(ctx, SettingsInput{Version: settings.Version, Enabled: true, Currency: "USD"}); !errors.Is(err, ErrConflict) {
		t.Fatal("currency change reinterpreted saved price", err)
	}
	if _, err = s.Publication(ctx, "first", "en", 1, true); err != nil {
		t.Fatal(err)
	}
	if _, err = s.SaveVariant(ctx, "first", "RED-1", VariantInput{Version: variant.Version, PriceMinor: 3000, Active: false}); !errors.Is(err, ErrVariant) {
		t.Fatal("last active variant deactivation", err)
	}
	addID, _ := db.NewID()
	restock := AdjustmentInput{ID: addID, Delta: 1, Reason: "Delivery"}
	original, err := s.Adjust(ctx, "owner", "RED-1", restock)
	if err != nil || original.Before != 0 || original.After != 1 {
		t.Fatal(original, err)
	}
	start := make(chan struct{})
	results := make(chan error, 2)
	var wg sync.WaitGroup
	for range 2 {
		id, _ := db.NewID()
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, err := s.Adjust(ctx, "owner", "RED-1", AdjustmentInput{ID: id, Delta: -1, Reason: "Stock correction"})
			results <- err
		}()
	}
	close(start)
	wg.Wait()
	close(results)
	var accepted, rejected int
	for err := range results {
		switch {
		case err == nil:
			accepted++
		case errors.Is(err, ErrStock):
			rejected++
		default:
			t.Fatal(err)
		}
	}
	if accepted != 1 || rejected != 1 {
		t.Fatal(accepted, rejected)
	}
	replayed, err := s.Adjust(ctx, "owner", "RED-1", restock)
	if err != nil || replayed != original {
		t.Fatal("retry changed the recorded adjustment", replayed, err)
	}
	restock.Delta = 2
	if _, err = s.Adjust(ctx, "owner", "RED-1", restock); !errors.Is(err, ErrConflict) {
		t.Fatal("changed retry was accepted", err)
	}
	history, err := s.Adjustments(ctx, "RED-1")
	if err != nil || len(history) != 2 {
		t.Fatal(history, err)
	}
	draft, err := s.Draft(ctx, "first", "en")
	if err != nil || draft.Variants[0].Stock != 0 {
		t.Fatal(draft, err)
	}
	public, err := s.PublicProduct(ctx, "en", "first")
	if err != nil || public.Product.Variants[0].InStock || public.Product.PriceMinor != 3000 {
		t.Fatal(public, err)
	}
}
