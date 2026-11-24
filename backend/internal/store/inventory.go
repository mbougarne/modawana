package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/mbougarne/modawana/backend/internal/access"
	"github.com/mbougarne/modawana/backend/internal/db"
	"github.com/mbougarne/modawana/backend/internal/locale"
	"github.com/mbougarne/modawana/backend/internal/media"
)

const maxStock = 1_000_000_000

type VariantInput struct {
	Version    int64             `json:"version"`
	Labels     map[string]string `json:"labels"`
	PriceMinor int64             `json:"price_minor"`
	Active     bool              `json:"active"`
}

type Variant struct {
	VariantInput
	SKU      string `json:"sku"`
	Stock    int64  `json:"stock"`
	Reserved int64  `json:"reserved"`
}

func validateVariant(product, sku string, in VariantInput) bool {
	if !keyPattern.MatchString(product) || !skuPattern.MatchString(sku) || in.Version < 0 ||
		in.PriceMinor < 1 || in.PriceMinor > 1_000_000_000_000 || len(in.Labels) > len(locale.Codes()) {
		return false
	}
	for language, label := range in.Labels {
		if !locale.Valid(language) || !plain(label, 120, true, false) {
			return false
		}
	}
	return true
}

func (s *Service) SaveVariant(ctx context.Context, product, sku string, in VariantInput) (Variant, error) {
	sku = strings.ToUpper(strings.TrimSpace(sku))
	if !validateVariant(product, sku, in) {
		return Variant{}, ErrInvalid
	}
	if in.Labels == nil {
		in.Labels = map[string]string{}
	}
	labels, _ := json.Marshal(in.Labels)
	var out Variant
	err := s.write(ctx, product, func(tx *sql.Tx) error {
		if in.Version == 0 {
			var count int
			if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM store_variants WHERE product=?", product).Scan(&count); err != nil {
				return err
			}
			if count >= 32 {
				return ErrInvalid
			}
			if _, err := tx.ExecContext(ctx, `INSERT INTO store_variants(sku,product,version,labels,price_minor,active)
				VALUES(?,?,1,?,?,?)`, sku, product, string(labels), in.PriceMinor, in.Active); err != nil {
				return err
			}
		} else {
			res, err := tx.ExecContext(ctx, `UPDATE store_variants SET version=version+1,labels=?,price_minor=?,active=?
				WHERE product=? AND sku=? AND version=?`, string(labels), in.PriceMinor, in.Active, product, sku, in.Version)
			if err = changed(res, err); err != nil {
				return err
			}
			if !in.Active {
				var invalid bool
				if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM store_publications WHERE product=?)
					AND NOT EXISTS(SELECT 1 FROM store_variants WHERE product=? AND active=1)`, product, product).Scan(&invalid); err != nil {
					return err
				}
				if invalid {
					return ErrVariant
				}
			}
		}
		out = Variant{SKU: sku, VariantInput: in}
		return tx.QueryRowContext(ctx, "SELECT version,stock,reserved FROM store_variants WHERE sku=?", sku).Scan(&out.Version, &out.Stock, &out.Reserved)
	})
	return out, err
}

func readVariants(ctx context.Context, tx *sql.Tx, product string, public bool) ([]Variant, error) {
	query := "SELECT sku,version,labels,price_minor,active,stock,reserved FROM store_variants WHERE product=?"
	if public {
		query += " AND active=1"
	}
	rows, err := tx.QueryContext(ctx, query+" ORDER BY sku", product)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Variant{}
	for rows.Next() {
		var item Variant
		var labels string
		if err = rows.Scan(&item.SKU, &item.Version, &labels, &item.PriceMinor, &item.Active, &item.Stock, &item.Reserved); err != nil {
			return nil, err
		}
		if err = json.Unmarshal([]byte(labels), &item.Labels); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

type AdjustmentInput struct {
	ID     string `json:"id"`
	Delta  int64  `json:"delta"`
	Reason string `json:"reason"`
}

type Adjustment struct {
	AdjustmentInput
	SKU     string `json:"sku"`
	Actor   string `json:"actor"`
	Before  int64  `json:"before"`
	After   int64  `json:"after"`
	Created string `json:"created_at"`
}

const adjustmentColumns = "id,sku,actor,delta,reason,before_quantity,after_quantity,created_at"

type scanner interface{ Scan(...any) error }

func readAdjustment(row scanner) (Adjustment, error) {
	var out Adjustment
	err := row.Scan(&out.ID, &out.SKU, &out.Actor, &out.Delta, &out.Reason, &out.Before, &out.After, &out.Created)
	return out, err
}

func (s *Service) Adjust(ctx context.Context, owner, sku string, in AdjustmentInput) (Adjustment, error) {
	if !skuPattern.MatchString(sku) || !media.ID.MatchString(in.ID) || in.Delta == 0 || in.Delta > maxStock ||
		in.Delta < -maxStock || !plain(in.Reason, 300, true, false) {
		return Adjustment{}, ErrInvalid
	}
	in.Reason = strings.TrimSpace(in.Reason)
	actor := access.Actor(ctx, owner)
	out := Adjustment{SKU: sku, Actor: actor, AdjustmentInput: in}
	err := s.write(ctx, sku, func(tx *sql.Tx) error {
		prior, err := readAdjustment(tx.QueryRowContext(ctx, "SELECT "+adjustmentColumns+" FROM store_stock_adjustments WHERE id=?", in.ID))
		if err == nil {
			if prior.SKU != sku || prior.Actor != actor || prior.AdjustmentInput != in {
				return ErrConflict
			}
			out = prior
			return nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		return recordAdjustment(ctx, tx, &out, time.Now())
	})
	return out, err
}

func recordAdjustment(ctx context.Context, tx *sql.Tx, out *Adjustment, now time.Time) error {
	var reserved int64
	if err := tx.QueryRowContext(ctx, "SELECT stock,reserved FROM store_variants WHERE sku=?", out.SKU).Scan(&out.Before, &reserved); err != nil {
		return err
	}
	out.After = out.Before + out.Delta
	if out.After < reserved || out.After > maxStock {
		return ErrStock
	}
	out.Created = db.UTC(now)
	if _, err := tx.ExecContext(ctx, "UPDATE store_variants SET stock=? WHERE sku=?", out.After, out.SKU); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO store_stock_adjustments
			(id,sku,actor,delta,before_quantity,after_quantity,reason,created_at) VALUES(?,?,?,?,?,?,?,?)`,
		out.ID, out.SKU, out.Actor, out.Delta, out.Before, out.After, out.Reason, out.Created)
	return err
}

func (s *Service) Adjustments(ctx context.Context, sku string) ([]Adjustment, error) {
	if !skuPattern.MatchString(sku) {
		return nil, ErrInvalid
	}
	rows, err := s.DB.QueryContext(ctx, "SELECT "+adjustmentColumns+" FROM store_stock_adjustments WHERE sku=? ORDER BY created_at DESC,id DESC LIMIT 50", sku)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Adjustment{}
	for rows.Next() {
		item, err := readAdjustment(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}
