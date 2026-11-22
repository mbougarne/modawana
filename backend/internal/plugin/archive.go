package plugin

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"

	"github.com/mbougarne/modawana/backend/internal/bundle"
)

type ArchiveRecord struct {
	Slug  string
	Files []File
}

func (a ArchiveRecord) Path(file File) string {
	return path.Join("plugins", a.Slug, file.Path)
}

func ArchiveRecords(ctx context.Context, database *sql.DB) ([]ArchiveRecord, error) {
	rows, err := database.QueryContext(ctx, "SELECT slug,files FROM plugin_installs ORDER BY slug")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var records []ArchiveRecord
	for rows.Next() {
		var a ArchiveRecord
		var files string
		if err := rows.Scan(&a.Slug, &files); err != nil {
			return nil, err
		}
		if !slugPattern.MatchString(a.Slug) || json.Unmarshal([]byte(files), &a.Files) != nil || (len(a.Files) == 0 || len(a.Files) > maxPluginFiles) {
			return nil, ErrFiles
		}
		var total int64
		seen := map[string]bool{}
		for _, f := range a.Files {
			if !fs.ValidPath(f.Path) || f.Path == "." || seen[f.Path] || f.Size < 0 ||
				f.Size > maxTotalBytes-total || !digestPattern.MatchString(f.SHA256) {
				return nil, ErrFiles
			}
			seen[f.Path] = true
			total += f.Size
		}
		records = append(records, a)
	}
	return records, rows.Err()
}

// CopyVerifiedFiles copies only an entire approved file set into a private
// backup staging tree. Missing or changed files leave the set absent.
func (a ArchiveRecord) CopyVerifiedFiles(source, destination string) ([]string, error) {
	root, err := os.OpenRoot(source)
	if err != nil {
		return nil, nil
	}
	defer root.Close()
	if info, err := root.Lstat(path.Join("plugins", a.Slug)); err != nil || !info.IsDir() {
		return nil, nil
	}
	directory, err := root.OpenRoot(path.Join("plugins", a.Slug))
	if err != nil {
		return nil, nil
	}
	defer directory.Close()
	staged := filepath.Join(destination, "plugins", a.Slug)
	keep := false
	defer func() {
		if !keep {
			_ = os.RemoveAll(staged)
		}
	}()
	files := make([]string, 0, len(a.Files))
	for _, file := range a.Files {
		key := a.Path(file)
		if !bundle.ValidPath(key) {
			return nil, nil
		}
		f, err := openRegular(directory, file.Path)
		if err != nil {
			return nil, nil
		}
		out := filepath.Join(destination, filepath.FromSlash(key))
		if err := os.MkdirAll(filepath.Dir(out), 0700); err != nil {
			f.Close()
			return nil, err
		}
		dst, err := os.OpenFile(out, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			f.Close()
			return nil, err
		}
		hash := sha256.New()
		n, err := io.Copy(io.MultiWriter(dst, hash), io.LimitReader(f, file.Size+1))
		f.Close()
		closed := dst.Close()
		if err != nil {
			return nil, err
		}
		if closed != nil {
			return nil, closed
		}
		if n != file.Size || hex.EncodeToString(hash.Sum(nil)) != file.SHA256 {
			return nil, nil
		}
		files = append(files, key)
	}
	keep = true
	return files, nil
}
