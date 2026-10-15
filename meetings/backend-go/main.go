package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"meeting-summarizer/handlers"
	"meeting-summarizer/store"

	"github.com/gorilla/mux"
)

func main() {
	port := os.Getenv("GO_BACKEND_PORT")
	if port == "" {
		port = "8080"
	}

	dbPath := os.Getenv("GO_DB_PATH")
	if dbPath == "" {
		dbPath = "/data/meetings.db"
	}

	// Ensure directories exist
	dirs := []string{"/data/uploads", "/data/exports"}
	if dir := filepath.Dir(dbPath); dir != "" {
		dirs = append(dirs, dir)
	}
	for _, dir := range dirs {
		os.MkdirAll(dir, 0755)
	}

	// Initialize SQLite store
	st, err := store.New(dbPath)
	if err != nil {
		log.Fatalf("Failed to create store: %v", err)
	}
	ctx := context.Background()
	if err := st.Open(ctx); err != nil {
		log.Fatalf("Failed to open store: %v", err)
	}
	defer st.Close()

	// Create handler
	h := handlers.NewHandler(st)

	// Setup routes
	r := mux.NewRouter()
	handlers.SetupRoutes(r, h)

	// Create HTTP server
	srv := &http.Server{
		Addr:         fmt.Sprintf(":%s", port),
		Handler:      r,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 600 * time.Second, // Long timeout for file uploads
		IdleTimeout:  120 * time.Second,
	}

	// Graceful shutdown
	go func() {
		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
		<-sigChan

		shutCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()

		log.Println("Shutting down...")
		if err := srv.Shutdown(shutCtx); err != nil {
			log.Printf("Server forced shutdown: %v", err)
		}
	}()

	// Start server
	log.Printf("Meeting Summarizer starting on http://localhost:%s\n", port)
	if err := srv.ListenAndServe(); err != http.ErrServerClosed {
		log.Fatalf("Server failed: %v", err)
	}
	log.Println("Server stopped gracefully")
}
