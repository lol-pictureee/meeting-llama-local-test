package handlers

import (
	"net/http"
	"os"
	"path/filepath"

	"github.com/gorilla/mux"
)

// SetupRoutes registers all HTTP routes on the given mux router.
func SetupRoutes(r *mux.Router, h *Handler) {
	r.HandleFunc("/api/health", h.HealthCheck).Methods("GET")

	// Встречи
	r.HandleFunc("/api/meetings/upload", h.Upload).Methods("POST")
	r.HandleFunc("/api/meetings", h.ListMeetings).Methods("GET")
	r.HandleFunc("/api/meetings/{id}", h.GetMeeting).Methods("GET")
	r.HandleFunc("/api/meetings/{id}/reprocess", h.Reprocess).Methods("POST")
	r.HandleFunc("/api/meetings/{id}/export", h.ExportSummary).Methods("GET")

	// Статика + SPA
	staticDir := h.frontendDir
	fileServer := http.FileServer(http.Dir(staticDir))
	r.PathPrefix("/static/").Handler(http.StripPrefix("/static/", fileServer))

	r.PathPrefix("/").HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		clean := filepath.Clean("/" + req.URL.Path)
		candidate := filepath.Join(staticDir, clean)

		// Отдаём существующий файл, иначе — index.html (SPA fallback)
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			http.ServeFile(w, req, candidate)
			return
		}
		http.ServeFile(w, req, filepath.Join(staticDir, "index.html"))
	})
}