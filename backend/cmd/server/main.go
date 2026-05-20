package main

import (
	"context"
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

	// Start in-memory upload queue with 4 workers.
	queue := handler.InitVoiceNoteQueue(d, 4)
	defer queue.Close()

	// Start extraction queue with 2 workers.
	extractionQueue := handler.InitExtractionQueue(d, 2)
	defer extractionQueue.Close()

	// Graceful shutdown context.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

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

	// Register eval endpoints when EVAL_TOKEN is set. These expose the real prompt
	// paths to the promptfoo harness for regression testing.
	// NEVER set EVAL_TOKEN in production — the startup code panics if APP_ENV=prod.
	evalMux := http.NewServeMux()
	if evalToken := os.Getenv("EVAL_TOKEN"); evalToken != "" {
		if os.Getenv("APP_ENV") == "prod" {
			panic("EVAL_TOKEN must NOT be set in production (APP_ENV=prod). Remove it immediately.")
		}
		slog.Warn("eval endpoints ENABLED — do NOT set EVAL_TOKEN in production")
		evalMux.Handle("POST /eval/extract", handler.RequireEvalToken(evalToken, handler.HandleEvalExtract))
		evalMux.Handle("POST /eval/generate-report", handler.RequireEvalToken(evalToken, handler.HandleEvalGenerateReport))
	}

	srv := &http.Server{
		Addr: ":" + port,
		Handler: sentryhttp.New(sentryhttp.Options{Repanic: true}).Handle(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Dispatch eval routes before the main handler.
			if len(r.URL.Path) > 5 && r.URL.Path[:5] == "/eval" {
				evalMux.ServeHTTP(w, r)
				return
			}
			handler.Handle(w, r)
		})),
	}

	// Graceful shutdown on SIGINT/SIGTERM.
	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		<-sigCh
		slog.Info("shutting down...")
		cancel()
		queue.Close()
		extractionQueue.Close()
		if err := srv.Shutdown(context.Background()); err != nil {
			slog.Error("shutdown error", "error", err)
		}
	}()

	slog.Info("server starting", "port", port)
	if err := srv.ListenAndServe(); err != http.ErrServerClosed {
		slog.Error("server failed", "error", err)
		cancel()
		queue.Close()
		extractionQueue.Close()
	}
}
