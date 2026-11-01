package main

import (
	"context"
	"errors"
	"fmt"
	"github.com/mbougarne/modawana/backend/internal/auth"
	"github.com/mbougarne/modawana/backend/internal/learning"
	"github.com/mbougarne/modawana/backend/internal/operations"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/mbougarne/modawana/backend/internal/access"
	"github.com/mbougarne/modawana/backend/internal/config"
	"github.com/mbougarne/modawana/backend/internal/content"
	"github.com/mbougarne/modawana/backend/internal/db"
	httpserver "github.com/mbougarne/modawana/backend/internal/http"
	"github.com/mbougarne/modawana/backend/internal/media"
	"github.com/mbougarne/modawana/backend/internal/siteconfig"
	"github.com/mbougarne/modawana/backend/internal/storage"
)

func main() { os.Exit(run()) }
func run() int {
	logger := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	cwd, err := os.Getwd()
	if err != nil {
		logger.Error("working directory unavailable")
		return 1
	}
	cfg, err := config.Load(os.Getenv, cwd)
	if err != nil {
		logger.Error("configuration rejected", "reason", err.Error())
		return 1
	}
	command, archive, err := operationArgs(os.Args[1:])
	if err != nil {
		logger.Error(err.Error())
		return 1
	}
	if command == "ready" {
		if err = readiness(cfg.ListenAddr); err != nil {
			return 1
		}
		return 0
	}
	local := command != ""
	if command == "restore" {
		if err = operations.Restore(context.Background(), archive, cfg.DataRoot); err != nil {
			logger.Error("restore rejected", "reason", err.Error())
			return 1
		}
		fmt.Fprintln(os.Stdout, "Restored in maintenance mode. Re-enroll and test a passkey, then run blog resume.")
		return 0
	}
	if os.Getenv("TMPDIR") == filepath.Join(cfg.DataRoot, "tmp") {
		if err = os.MkdirAll(os.Getenv("TMPDIR"), 0700); err != nil {
			logger.Error("temporary directory unavailable")
			return 1
		}
	}
	var assets *httpserver.Assets
	if cfg.Environment == "production" && !local {
		assets, err = httpserver.LoadAssets(cfg.FrontendDist)
		if err != nil {
			logger.Error("frontend build rejected")
			return 1
		}
	}
	var files *storage.Store
	if !local {
		if err = os.MkdirAll(cfg.DataRoot, 0700); err != nil {
			logger.Error("data directory unavailable")
			return 1
		}
		files, err = storage.Open(cfg.DataRoot)
		if err != nil {
			logger.Error("storage initialization failed")
			return 1
		}
		defer files.Close()
		lease, err := files.Lease()
		if err != nil {
			logger.Error("another server already owns this data directory")
			return 1
		}
		defer lease.Close()
	}
	startup, cancelStartup := context.WithTimeout(context.Background(), 30*time.Second)
	database, err := db.Open(startup, cfg.DataRoot)
	cancelStartup()
	if err != nil {
		logger.Error("database initialization failed; verify migration history, private directory permissions and backup compatibility")
		return 1
	}
	defer database.Close()
	if local && command != "bootstrap" && command != "recover" && command != "cleanup-learning" {
		if err = operate(context.Background(), command, archive, cfg.DataRoot, database); err != nil {
			logger.Error("operation failed", "reason", err.Error())
			return 1
		}
		return 0
	}
	if local && command == "cleanup-learning" {
		identities := learning.NewIdentity(database, cfg.LearningIdleDays, cfg.LearningMaxIdentities)
		for {
			n, e := identities.Cleanup(context.Background())
			if e != nil {
				logger.Error("learning cleanup failed")
				return 1
			}
			if n == 0 {
				break
			}
		}
		fmt.Fprintln(os.Stdout, "Learning cleanup complete.")
		return 0
	}
	authentication := auth.New(database, cfg)
	if local {
		grant, e := authentication.Grant(context.Background(), command == "recover")
		if e != nil {
			if command == "bootstrap" && errors.Is(e, auth.ErrConflict) {
				logger.Error("owner already exists; use local recover with the same data root to finish enrollment or replace a lost factor")
				return 1
			}
			logger.Error("local grant failed")
			return 1
		}
		fmt.Fprintln(os.Stdout, grant)
		return 0
	}
	policy := &access.Live{DB: database}
	contents := &content.Service{DB: database, Files: files, Access: policy}
	contents.Membership = contents.AttachMember
	contents.PublicationHook = contents.CheckPublicationPlan
	policy.Gates = contents.LessonAccess
	settings := &siteconfig.Service{DB: database, Files: files}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	identities := learning.NewIdentity(database, cfg.LearningIdleDays, cfg.LearningMaxIdentities)
	go func() {
		ticker := time.NewTicker(time.Hour)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if _, e := operations.Cleanup(ctx, database, files); e != nil {
					logger.Error("maintenance batch failed")
				}
			}
		}
	}()
	domain := &httpserver.Domain{Learning: identities, SiteConfig: settings, Content: contents, Auth: authentication, Media: media.New(database, files, policy)}
	if cfg.Environment == "production" && len(cfg.TrustedProxies) == 0 {
		logger.Warn("no trusted proxies configured; reverse-proxy deployments must set BLOG_TRUSTED_PROXIES to observed proxy peers for per-client throttling")
	}
	handler := httpserver.New(httpserver.Options{Maintenance: func() bool { return operations.Maintenance(cfg.DataRoot) }, SiteConfig: settings, Config: cfg, Assets: assets, Logger: logger, Ready: database.PingContext, Auth: authentication, Domain: domain})
	srv := &http.Server{Addr: cfg.ListenAddr, Handler: handler, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16 << 10, ErrorLog: slog.NewLogLogger(logger.Handler(), slog.LevelError)}
	done := make(chan error, 1)
	go func() { done <- srv.ListenAndServe() }()
	select {
	case err = <-done:
		if !errors.Is(err, http.ErrServerClosed) {
			logger.Error("HTTP server stopped")
			return 1
		}
	case <-ctx.Done():
		shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if srv.Shutdown(shutdown) != nil {
			_ = srv.Close()
			logger.Error("HTTP shutdown timed out")
			return 1
		}
	}
	return 0
}
