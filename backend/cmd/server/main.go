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
	"github.com/joho/godotenv"
	handler "github.com/nicogaller/gradebee/backend"
)

func main() {
	// Load .env if present (local dev). In Docker, env vars come from the container.
	if err := godotenv.Load("../../../.env"); err != nil && !os.IsNotExist(err) {
		slog.Warn("loading .env", "error", err)
	}

	if os.Getenv("CLERK_SECRET_KEY") == "" {
		panic("CLERK_SECRET_KEY is not set")
	}
	clerk.SetKey(os.Getenv("CLERK_SECRET_KEY"))

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
	queue := handler.InitUploadQueue(d, 4)
	defer queue.Close()

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
	uploadRepo := d.GetUploadRepo()
	go handler.StartUploadCleanup(ctx, uploadRepo, uploadsDir, time.Duration(retentionHours)*time.Hour, 1*time.Hour)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	srv := &http.Server{
		Addr:    ":" + port,
		Handler: http.HandlerFunc(handler.Handle),
	}

	// Graceful shutdown on SIGINT/SIGTERM.
	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		<-sigCh
		slog.Info("shutting down...")
		cancel()
		queue.Close()
		if err := srv.Shutdown(context.Background()); err != nil {
			slog.Error("shutdown error", "error", err)
		}
	}()

	slog.Info("server starting", "port", port)
	if err := srv.ListenAndServe(); err != http.ErrServerClosed {
		slog.Error("server failed", "error", err)
		cancel()
		queue.Close()
	}
}
