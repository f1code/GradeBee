package main

import (
	"log/slog"
	"net/http"
	"os"

	"github.com/joho/godotenv"
	handler "github.com/nicogaller/gradebee/backend"
)

func main() {
	// Load .env from project root when running locally (../.env relative to backend/)
	if err := godotenv.Load("../.env"); err != nil && !os.IsNotExist(err) {
		slog.Warn("loading .env", "error", err)
	}

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	slog.Info("server starting", "port", port)
	if err := http.ListenAndServe(":"+port, http.HandlerFunc(handler.Handle)); err != nil {
		slog.Error("server failed", "error", err)
		os.Exit(1)
	}
}
