package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/clerk/clerk-sdk-go/v2"
	"github.com/getsentry/sentry-go"
	sentryhttp "github.com/getsentry/sentry-go/http"
	"github.com/joho/godotenv"
	handler "github.com/nicogaller/gradebee/backend"
)

func main() {
	// Load .env if present (local dev). In Docker, env vars come from the container.
	if err := godotenv.Load("../../../.env"); err != nil && !os.IsNotExist(err) {
		slog.Warn("loading .env", "error", err)
	}

	// Initialise Sentry error reporting (no-op if SENTRY_DSN is unset).
	handler.InitSentry()
	defer sentry.Flush(2 * time.Second)
	// Wire the package logger; must come after InitSentry so the sentryslog
	// handler can attach to the already-configured Sentry client.
	handler.InitLogger()

	// --migrate-only: run DB migrations and exit (used by Dokku predeploy hook).
	migrateOnly := len(os.Args) > 1 && os.Args[1] == "--migrate-only"

	if !migrateOnly {
		if os.Getenv("CLERK_SECRET_KEY") == "" {
			panic("CLERK_SECRET_KEY is not set")
		}
		clerk.SetKey(os.Getenv("CLERK_SECRET_KEY"))
	}

	// Open SQLite database and run migrations.
	dbPath := os.Getenv("DB_PATH")
	if dbPath == "" {
		dbPath = "/data/gradebee.db"
	}
	db, err := handler.OpenDB(dbPath)
	if err != nil {
		panic("open db: " + err.Error())
	}
	defer db.Close()

	if err := handler.RunMigrations(db); err != nil {
		panic("run migrations: " + err.Error())
	}

	if migrateOnly {
		slog.Info("migrations complete")
		return
	}

	// Uploads directory.
	uploadsDir := os.Getenv("UPLOADS_DIR")
	if uploadsDir == "" {
		uploadsDir = "/data/uploads"
	}
	if err := os.MkdirAll(uploadsDir, 0o755); err != nil {
		panic("create uploads dir: " + err.Error())
	}

	// Initialize dependencies with DB handle and uploads dir.
	d := handler.NewProdDeps(db, uploadsDir)

	// Start in-memory upload queue with 4 workers. Closed explicitly during
	// shutdown (below) so it drains after the HTTP server stops accepting.
	queue := handler.InitVoiceNoteQueue(d, 4)

	// Context for background goroutines (cleanup loop). Cancelled last in
	// the shutdown sequence.
	ctx, cancel := context.WithCancel(context.Background())

	// Start upload cleanup goroutine.
	retentionHours := 168 // 7 days default
	if env := os.Getenv("UPLOAD_RETENTION_HOURS"); env != "" {
		if h, err := strconv.Atoi(env); err == nil && h > 0 {
			retentionHours = h
		}
	}
	voiceNoteRepo := d.GetVoiceNoteRepo()
	go handler.StartVoiceNoteCleanup(ctx, voiceNoteRepo, time.Duration(retentionHours)*time.Hour, 1*time.Hour)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", handler.Handle)

	srv := &http.Server{
		Addr:    ":" + port,
		Handler: sentryhttp.New(sentryhttp.Options{Repanic: true}).Handle(mux),
		// Bound slow or idle clients. No WriteTimeout: upload and LLM-backed
		// handlers legitimately run for minutes, and their upstream calls
		// carry their own deadlines at the provider boundary.
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	// Serve in the background; the error (nil on clean Shutdown) is reported
	// on errCh so main can select between it and a termination signal.
	errCh := make(chan error, 1)
	go func() {
		slog.Info("server starting", "port", port)
		errCh <- srv.ListenAndServe()
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	select {
	case sig := <-sigCh:
		slog.Info("shutting down...", "signal", sig.String())
	case err := <-errCh:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("server failed", "error", err)
		}
	}

	// Shutdown sequence, in order:
	//   1. Stop accepting connections and wait (up to 30s) for in-flight
	//      requests to finish.
	//   2. Drain the job queue: Close blocks until running workers return.
	//   3. Cancel the background ctx (cleanup loop).
	// Deferred db.Close and sentry.Flush then run as main returns.
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer shutdownCancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		slog.Error("shutdown error", "error", err)
	}
	queue.Close()
	cancel()
	slog.Info("shutdown complete")
}
