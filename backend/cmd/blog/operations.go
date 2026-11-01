package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"flag"
	"github.com/mbougarne/modawana/backend/internal/operations"
	"github.com/mbougarne/modawana/backend/internal/portability"
	"github.com/mbougarne/modawana/backend/internal/storage"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func operationArgs(args []string) (string, string, error) {
	if len(args) == 0 {
		return "", "", nil
	}
	command := args[0]
	switch command {
	case "bootstrap", "recover", "cleanup-learning", "maintenance", "resume", "ready":
		if len(args) != 1 {
			return "", "", errors.New("unexpected command arguments")
		}
		return command, "", nil
	case "export", "import", "backup", "restore":
		f := flag.NewFlagSet(command, flag.ContinueOnError)
		f.SetOutput(io.Discard)
		file := f.String("file", "", "archive path")
		if f.Parse(args[1:]) != nil || f.NArg() != 0 || !filepath.IsAbs(*file) {
			return "", "", errors.New("command requires --file /absolute/archive.zip")
		}
		return command, *file, nil
	default:
		return "", "", errors.New("usage: blog [bootstrap|recover|cleanup-learning|maintenance|resume|export|import|backup|restore]")
	}
}
func operate(ctx context.Context, command, archive, root string, d *sql.DB) error {
	files, e := storage.Open(root)
	if e != nil {
		return e
	}
	defer files.Close()
	switch command {
	case "maintenance":
		out, e := operations.Cleanup(ctx, d, files)
		if e != nil {
			return e
		}
		return json.NewEncoder(os.Stdout).Encode(out)
	case "resume":
		return operations.Resume(ctx, d, root)
	case "export", "backup":
		rel, e := filepath.Rel(root, archive)
		if e != nil || rel == "." || (!strings.HasPrefix(rel, ".."+string(filepath.Separator)) && rel != "..") {
			return errors.New("archive must be outside the data directory")
		}
		f, e := os.OpenFile(archive, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if e != nil {
			return e
		}
		good := false
		defer func() {
			f.Close()
			if !good {
				os.Remove(archive)
			}
		}()
		if command == "backup" {
			e = operations.Backup(ctx, d, files, f)
		} else {
			e = (&portability.Service{DB: d, Files: files}).Export(ctx, f)
		}
		if e != nil {
			return e
		}
		if e = f.Sync(); e != nil {
			return e
		}
		good = true
		return nil
	case "import":
		var owner string
		if e = d.QueryRowContext(ctx, "SELECT id FROM owner").Scan(&owner); e != nil {
			return errors.New("set up the owner before importing")
		}
		out, e := (&portability.Service{DB: d, Files: files}).Import(ctx, owner, archive, nil)
		if e != nil {
			return e
		}
		return json.NewEncoder(os.Stdout).Encode(out)
	}
	return errors.New("unknown operation")
}

func readiness(address string) error {
	_, port, e := net.SplitHostPort(address)
	if e != nil {
		return e
	}
	client := &http.Client{Timeout: 3 * time.Second}
	r, e := client.Get("http://127.0.0.1:" + port + "/readyz")
	if e != nil {
		return e
	}
	defer r.Body.Close()
	if r.StatusCode != 200 {
		return errors.New("server not ready")
	}
	return nil
}
