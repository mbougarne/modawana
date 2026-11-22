package theme

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"os"
	"sort"

	"github.com/mbougarne/modawana/backend/internal/db"
)

const maxInstalledPacks = 64
const maxPackDirectoryEntries = 128

type Service struct {
	DB       *sql.DB
	DataRoot string
}

type Approval struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Checksum string `json:"checksum"`
	Revision string `json:"revision"`
}

type Status struct {
	Approval
	State             string `json:"state"`
	CandidateName     string `json:"candidate_name,omitempty"`
	CandidateChecksum string `json:"candidate_checksum,omitempty"`
}

type Query interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

func approval(ctx context.Context, db Query, id string) (Approval, error) {
	out := Approval{ID: id}
	err := db.QueryRowContext(ctx, "SELECT name,checksum,revision FROM theme_packs WHERE id=?", id).
		Scan(&out.Name, &out.Checksum, &out.Revision)
	return out, err
}

func Approvals(ctx context.Context, db Query) ([]Approval, error) {
	rows, err := db.QueryContext(ctx, "SELECT id,name,checksum,revision FROM theme_packs ORDER BY id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Approval{}
	for rows.Next() {
		var a Approval
		if err = rows.Scan(&a.ID, &a.Name, &a.Checksum, &a.Revision); err != nil {
			return nil, err
		}
		if !IsPack(a.ID) || len(out) >= maxInstalledPacks {
			return nil, ErrPack
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (s *Service) ApprovedPack(ctx context.Context, q Query, id string) (Pack, error) {
	if s == nil || !IsPack(id) {
		return Pack{}, ErrPack
	}
	a, err := approval(ctx, q, id)
	if err != nil {
		return Pack{}, err
	}
	return ReadVerifiedPack(s.DataRoot, id, a.Checksum)
}

// Appearance falls back to Classic for unavailable approvals. Database errors
// remain distinguishable from a missing, invalid or changed file.
func (s *Service) Appearance(ctx context.Context, q Query, id string) (Appearance, bool, error) {
	builtin, ok := Lookup(id)
	if ok {
		return builtin, true, nil
	}
	pack, err := s.ApprovedPack(ctx, q, id)
	if err == nil {
		return pack.Appearance, true, nil
	}
	if errors.Is(err, sql.ErrNoRows) || errors.Is(err, ErrPack) || errors.Is(err, ErrPackFiles) || errors.Is(err, ErrPackChanged) {
		return builtin, false, nil
	}
	return Appearance{}, false, err
}

func (s *Service) List(ctx context.Context) ([]Status, error) {
	approved, err := Approvals(ctx, s.DB)
	if err != nil {
		return nil, err
	}
	known := map[string]bool{}
	items := make([]Status, 0, len(approved))
	for _, a := range approved {
		known[a.ID] = true
		items = append(items, s.status(a))
	}
	root, err := os.OpenRoot(s.DataRoot)
	if err != nil {
		return nil, ErrPackFiles
	}
	defer root.Close()
	info, err := root.Lstat("themes")
	if errors.Is(err, os.ErrNotExist) {
		return items, nil
	}
	if err != nil || !info.IsDir() {
		return nil, ErrPackFiles
	}
	dir, err := root.Open("themes")
	if err != nil {
		return nil, ErrPackFiles
	}
	defer dir.Close()
	entries, err := dir.ReadDir(maxPackDirectoryEntries + 1)
	if (err != nil && err != io.EOF) || len(entries) > maxPackDirectoryEntries {
		return nil, ErrPackFiles
	}
	for _, entry := range entries {
		id := entry.Name()
		if entry.IsDir() && IsPack(id) && !known[id] {
			items = append(items, s.status(Approval{ID: id}))
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	return items, nil
}

func (s *Service) status(a Approval) Status {
	item := Status{Approval: a}
	pack, err := ReadPack(s.DataRoot, a.ID)
	switch {
	case errors.Is(err, ErrPackFiles):
		item.State = "missing"
	case err != nil:
		item.State = "invalid"
	case pack.Checksum == a.Checksum:
		item.State = "installed"
	default:
		item.State = "available"
		if a.Revision != "" {
			item.State = "changed"
		}
		item.CandidateName, item.CandidateChecksum = pack.Name, pack.Checksum
	}
	return item
}

// Approve records the exact candidate shown to the owner. The approval
// revision guards subsequent updates and removals, even across a reinstall.
func (s *Service) Approve(ctx context.Context, id, checksum, expected string, check func(*sql.Tx) error) (Approval, error) {
	if !packChecksum.MatchString(checksum) {
		return Approval{}, ErrPack
	}
	pack, err := ReadVerifiedPack(s.DataRoot, id, checksum)
	if err != nil {
		return Approval{}, err
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return Approval{}, err
	}
	defer tx.Rollback()
	if err = check(tx); err != nil {
		return Approval{}, err
	}
	current, err := approval(ctx, tx, id)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return Approval{}, err
	}
	if current.Revision != expected {
		return Approval{}, db.ErrVersionConflict
	}
	if current.Checksum == checksum {
		return current, nil
	}
	if current.Revision == "" {
		var count int
		if err = tx.QueryRowContext(ctx, "SELECT count(*) FROM theme_packs").Scan(&count); err != nil {
			return Approval{}, err
		}
		if count >= maxInstalledPacks {
			return Approval{}, ErrPack
		}
	}
	revision, err := db.NewID()
	if err != nil {
		return Approval{}, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO theme_packs(id,name,checksum,revision) VALUES(?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET name=excluded.name,checksum=excluded.checksum,revision=excluded.revision`,
		id, pack.Name, checksum, revision); err != nil {
		return Approval{}, err
	}
	if err = tx.Commit(); err != nil {
		return Approval{}, err
	}
	return Approval{ID: id, Name: pack.Name, Checksum: checksum, Revision: revision}, nil
}

func (s *Service) Remove(ctx context.Context, id, expected string, check func(*sql.Tx) error) error {
	if !IsPack(id) || expected == "" {
		return ErrPack
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = check(tx); err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, "DELETE FROM theme_packs WHERE id=? AND revision=?", id, expected)
	if err != nil {
		return err
	}
	removed, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if removed != 1 {
		return db.ErrVersionConflict
	}
	return tx.Commit()
}
