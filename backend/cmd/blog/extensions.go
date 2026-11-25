package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/mbougarne/modawana/backend/internal/catalog"
	"github.com/mbougarne/modawana/backend/internal/config"
	"github.com/mbougarne/modawana/backend/internal/db"
	"github.com/mbougarne/modawana/backend/internal/extension"
	"github.com/mbougarne/modawana/backend/internal/plugin"
	"github.com/mbougarne/modawana/backend/internal/storage"
	"github.com/mbougarne/modawana/backend/internal/theme"
)

func extensionCommand(cfg config.Config, args []string, out io.Writer) error {
	const usage = "usage: blog extension preview|install --catalog PATH --signature PATH --keyring PATH --bundle PATH --kind plugin|theme --id ID --version VERSION [--approve REVIEW_SHA256]"
	if len(args) == 0 || (args[0] != "preview" && args[0] != "install") {
		return errors.New(usage)
	}
	var req catalog.Request
	flags := flag.NewFlagSet(args[0], flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	flags.StringVar(&req.Index, "catalog", "", "signed index")
	flags.StringVar(&req.Signature, "signature", "", "signature envelope")
	flags.StringVar(&req.Keyring, "keyring", "", "operator-trusted keys")
	flags.StringVar(&req.Bundle, "bundle", "", "extension bundle")
	flags.StringVar(&req.Kind, "kind", "", "extension kind")
	flags.StringVar(&req.ID, "id", "", "extension slug or theme ID")
	flags.StringVar(&req.Version, "version", "", "catalog version")
	approval := flags.String("approve", "", "preview SHA-256")
	if flags.Parse(args[1:]) != nil || flags.NArg() != 0 || req.Index == "" || req.Signature == "" ||
		req.Keyring == "" || req.Bundle == "" || req.Kind == "" || req.ID == "" || req.Version == "" ||
		(args[0] == "install" && len(*approval) != 64) || (args[0] == "preview" && *approval != "") {
		return errors.New(usage)
	}
	if err := os.MkdirAll(cfg.DataRoot, 0700); err != nil {
		return errors.New("extension data directory unavailable")
	}
	files, err := storage.Open(cfg.DataRoot)
	if err != nil {
		return err
	}
	defer files.Close()
	lease, err := files.Lease()
	if err != nil {
		return errors.New("stop the server before reviewing or installing an extension")
	}
	defer lease.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	database, err := db.Open(ctx, cfg.DataRoot)
	if err != nil {
		return err
	}
	defer database.Close()
	runtime, err := plugin.NewRuntime(ctx, database)
	if err != nil {
		return err
	}
	defer runtime.Close(context.Background())
	runtime.SetKeySource(cfg.ReadAuthKey)
	installer := catalog.Installer{
		Root:    cfg.DataRoot,
		Plugins: &plugin.Service{DB: database, Dir: filepath.Join(cfg.DataRoot, "plugins"), Host: extension.NewHost(nil), Runtime: runtime},
		Themes:  &theme.Service{DB: database, DataRoot: cfg.DataRoot},
	}
	return files.Exclusive(ctx, func() error {
		review, err := installer.Run(ctx, req, *approval, time.Now())
		if err != nil {
			return err
		}
		encoder := json.NewEncoder(out)
		encoder.SetIndent("", "  ")
		return encoder.Encode(review)
	})
}
