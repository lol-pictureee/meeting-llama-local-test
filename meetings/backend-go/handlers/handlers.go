package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"runtime/debug"
	"strings"

	"meeting-summarizer/client"
	"meeting-summarizer/models"
	"meeting-summarizer/store"

	"github.com/gorilla/mux"
)

const maxUploadSize = 200 * 1024 * 1024 // 200 MB

// Handler holds dependencies for HTTP request handling.
type Handler struct {
	store       *store.Store
	whisper     *client.WhisperClient
	ollama      *client.OllamaClient
	uploadsDir  string
	exportsDir  string
	frontendDir string
}

// NewHandler creates a new Handler from environment configuration.
func NewHandler(s *store.Store) *Handler {
	return &Handler{
		store:       s,
		whisper:     client.NewWhisperClient(),
		ollama:      client.NewOllamaClient(),
		uploadsDir:  getEnvOrDefault("GO_UPLOADS_DIR", "/data/uploads"),
		exportsDir:  getEnvOrDefault("GO_EXPORTS_DIR", "/data/exports"),
		frontendDir: getEnvOrDefault("FRONTEND_STATIC_DIR", "./frontend-static"),
	}
}

func getEnvOrDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// AllowedExtensions lists accepted audio file extensions.
var allowedExtensions = map[string]bool{
	".wav": true, ".mp3": true, ".m4a": true, ".mp4": true, ".webm": true,
}

// --- Public routes ---

// Upload handles POST /api/meetings/upload with a multipart form field "file".
func (h *Handler) Upload(w http.ResponseWriter, r *http.Request) {
	// Limit body size
	r.Body = http.MaxBytesReader(w, r.Body, maxUploadSize)

	err := r.ParseMultipartForm(maxUploadSize)
	if err != nil {
		http.Error(w, `{"error":"File too large or invalid form"}`, http.StatusBadRequest)
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		http.Error(w, `{"error":"No file provided"}`, http.StatusBadRequest)
		return
	}
	defer file.Close()

	ext := strings.ToLower(filepath.Ext(header.Filename))
	if !allowedExtensions[ext] {
		http.Error(w, fmt.Sprintf(`{"error":"Unsupported extension %s"}`, ext), http.StatusUnsupportedMediaType)
		return
	}

	data, err := io.ReadAll(file)
	if err != nil {
		http.Error(w, `{"error":"Failed to read file"}`, http.StatusInternalServerError)
		return
	}

	// Sanitize filename
	safeName := sanitizeFileName(header.Filename)
	filepath := filepath.Join(h.uploadsDir, safeName)

	// Save original file
	if err := os.WriteFile(filepath, data, 0644); err != nil {
		http.Error(w, `{"error":"Failed to save file"}`, http.StatusInternalServerError)
		return
	}

	// Create meeting record
	meeting := &models.Meeting{
		Filename:     safeName,
		OriginalName: safeName,
		SizeBytes:    int64(len(data)),
		Status:       models.StatusLoaded,
	}
	ctx := r.Context()
	if err := h.store.CreateMeeting(ctx, meeting); err != nil {
		os.Remove(filepath)
		http.Error(w, `{"error":"Failed to create meeting record"}`, http.StatusInternalServerError)
		return
	}

	// Запускаем обработку в фоне (HTTP-ответ не блокируется)
	h.StartProcessing(meeting.ID, safeName, data)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"id":         meeting.ID,
		"filename":   safeName,
		"size_bytes": len(data),
		"status":     string(models.StatusLoaded),
		"message":    "Processing started",
	})
}

// StartProcessing запускает полный пайплайн (Whisper → Gemma) в отдельной горутине.
// Использует собственный контекст: контекст HTTP-запроса отменяется сразу после ответа.
func (h *Handler) StartProcessing(meetingID int64, filename string, audioData []byte) {
	go func() {
		defer func() {
			if rec := recover(); rec != nil {
				log.Printf("[process] Meeting %d panicked: %v\n%s", meetingID, rec, debug.Stack())
				errMsg := fmt.Sprintf("Internal error: %v", rec)
				h.store.UpdateMeetingStatus(context.Background(), meetingID, models.StatusError, strPtr(errMsg))
			}
		}()
		h.runPipeline(context.Background(), meetingID, filename, audioData)
	}()
}

// runPipeline выполняет транскрипцию и анализ, обновляя статус встречи на каждом шаге.
func (h *Handler) runPipeline(ctx context.Context, meetingID int64, filename string, audioData []byte) {
	// --- Шаг 1: локальное распознавание речи (Whisper) ---
	log.Printf("[process] Meeting %d (%s): start transcription", meetingID, filename)
	h.store.UpdateMeetingStatus(ctx, meetingID, models.StatusTranscribing, nil)

	segments, err := h.whisper.Transcribe(ctx, filename, audioData)
	if err != nil {
		msg := "Whisper failed: " + err.Error()
		log.Printf("[process] Meeting %d: %v", meetingID, err)
		h.store.UpdateMeetingStatus(ctx, meetingID, models.StatusError, strPtr(msg))
		return
	}
	log.Printf("[process] Meeting %d: got %d segments from whisper", meetingID, len(segments))

	if err := h.store.SaveTranscriptSegments(ctx, meetingID, segments); err != nil {
		msg := "Failed to save transcript: " + err.Error()
		log.Printf("[process] Meeting %d: %v", meetingID, err)
		h.store.UpdateMeetingStatus(ctx, meetingID, models.StatusError, strPtr(msg))
		return
	}

	fullText, err := h.store.FullText(ctx, meetingID)
	if err != nil {
		msg := "Failed to read transcript: " + err.Error()
		log.Printf("[process] Meeting %d: %v", meetingID, err)
		h.store.UpdateMeetingStatus(ctx, meetingID, models.StatusError, strPtr(msg))
		return
	}
	if strings.TrimSpace(fullText) == "" {
		log.Printf("[process] Meeting %d: empty transcript", meetingID)
		h.store.UpdateMeetingStatus(ctx, meetingID, models.StatusError, strPtr("Empty transcription"))
		return
	}

	// --- Шаг 2: локальный анализ текста (Ollama / gemma4:12b) ---
	log.Printf("[process] Meeting %d: start LLM analysis (%d chars)", meetingID, len(fullText))
	h.store.UpdateMeetingStatus(ctx, meetingID, models.StatusAnalyzing, nil)

	rawResponse, err := h.ollama.GenerateSummary(ctx, fullText)
	if err != nil {
		msg := "LLM error: " + err.Error()
		log.Printf("[process] Meeting %d: %v", meetingID, err)
		h.store.UpdateMeetingStatus(ctx, meetingID, models.StatusError, strPtr(msg))
		return
	}

	summary, err := ParseSummary(rawResponse)
	if err != nil {
		msg := "LLM response is not valid JSON: " + err.Error()
		log.Printf("[process] Meeting %d: %v", meetingID, err)
		h.store.UpdateMeetingStatus(ctx, meetingID, models.StatusError, strPtr(msg))
		return
	}

	summaryJSON, err := json.Marshal(summary)
	if err != nil {
		msg := "Failed to serialize summary: " + err.Error()
		log.Printf("[process] Meeting %d: %v", meetingID, err)
		h.store.UpdateMeetingStatus(ctx, meetingID, models.StatusError, strPtr(msg))
		return
	}

	if err := h.store.SaveSummary(ctx, meetingID, string(summaryJSON), fullText); err != nil {
		msg := "Failed to save summary: " + err.Error()
		log.Printf("[process] Meeting %d: %v", meetingID, err)
		h.store.UpdateMeetingStatus(ctx, meetingID, models.StatusError, strPtr(msg))
		return
	}

	h.store.UpdateMeetingStatus(ctx, meetingID, models.StatusCompleted, nil)
	log.Printf("[process] Meeting %d: DONE successfully", meetingID)
}

// Reprocess повторно запускает обработку уже загруженного файла.
// Полезно после исправления окружения или при ошибке на предыдущем шаге.
func (h *Handler) Reprocess(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id, err := parseID(vars["id"])
	if err != nil {
		writeJSONError(w, "Invalid meeting id", http.StatusBadRequest)
		return
	}

	ctx := r.Context()
	meeting, err := h.store.GetMeeting(ctx, id)
	if err != nil {
		writeJSONError(w, "Meeting not found", http.StatusNotFound)
		return
	}

	// Ищем файл на диске: сначала по сохранённому имени, затем по original_name
	candidates := []string{meeting.Filename, meeting.OriginalName, sanitizeFileName(meeting.OriginalName)}
	var audioData []byte
	var usedName string
	for _, name := range candidates {
		if name == "" {
			continue
		}
		path := filepath.Join(h.uploadsDir, name)
		data, readErr := os.ReadFile(path)
		if readErr == nil && len(data) > 0 {
			audioData = data
			usedName = name
			break
		}
	}
	if audioData == nil {
		writeJSONError(w, "Audio file not found on disk", http.StatusNotFound)
		return
	}

	// Сбрасываем прошлый результат и ошибку перед повторным запуском
	if err := h.store.UpdateMeetingStatus(ctx, id, models.StatusLoaded, nil); err != nil {
		writeJSONError(w, "Failed to reset status: "+err.Error(), http.StatusInternalServerError)
		return
	}
	h.store.DeleteSummary(ctx, id)

	h.StartProcessing(id, usedName, audioData)
	log.Printf("[reprocess] Meeting %d: restarted with file %s (%d bytes)", id, usedName, len(audioData))

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"id":      id,
		"status":  string(models.StatusLoaded),
		"message": "Reprocessing started",
	})
}

// GetMeeting retrieves a single meeting with optional transcript and summary.
func (h *Handler) GetMeeting(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id, _ := parseID(vars["id"])

	meeting, err := h.store.GetMeeting(r.Context(), id)
	if err != nil {
		http.Error(w, `{"error":"Meeting not found"}`, http.StatusNotFound)
		return
	}

	result := map[string]interface{}{
		"meeting": meeting,
	}

	segments, _ := h.store.GetTranscriptSegments(r.Context(), id)
	result["segments"] = segments

	summary, _ := h.store.GetSummary(r.Context(), id)
	if summary != nil {
		var parsed models.SummaryJSON
		if err := json.Unmarshal([]byte(summary.JSON), &parsed); err == nil {
			result["summary"] = parsed
		} else {
			result["summary_json_raw"] = summary.JSON
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}

// ListMeetings returns all meetings.
func (h *Handler) ListMeetings(w http.ResponseWriter, r *http.Request) {
	limit := 50
	if q := r.URL.Query().Get("limit"); q != "" {
		fmt.Sscanf(q, "%d", &limit)
		if limit <= 0 || limit > 200 {
			limit = 50
		}
	}
	meetings, err := h.store.ListMeetings(r.Context(), limit)
	if err != nil {
		http.Error(w, `{"error":"Failed to list meetings"}`, http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(meetings)
}

// ExportSummary downloads a meeting's summary in the requested format.
func (h *Handler) ExportSummary(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id, _ := parseID(vars["id"])
	formatStr := r.URL.Query().Get("format")

	if formatStr != "json" && formatStr != "markdown" {
		formatStr = "json"
	}

	_, err := h.store.GetMeeting(r.Context(), id)
	if err != nil {
		http.Error(w, `{"error":"Meeting not found"}`, http.StatusNotFound)
		return
	}

	summary, err := h.store.GetSummary(r.Context(), id)
	if err != nil || summary == nil {
		http.Error(w, `{"error":"No summary available"}`, http.StatusNotFound)
		return
	}

	var content []byte
	var contentType string
	var filename string

	switch formatStr {
	case "json":
		contentType = "application/json"
		filename = fmt.Sprintf("summary-%d.json", id)
		content = []byte(summary.JSON)
	case "markdown":
		contentType = "text/markdown"
		filename = fmt.Sprintf("summary-%d.md", id)
		content = h.formatMarkdown(summary)
	default:
		contentType = "application/octet-stream"
		filename = fmt.Sprintf("summary-%d", id)
		content = []byte(summary.JSON)
	}

	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
	w.Write(content)
}

func (h *Handler) formatMarkdown(summary *struct {
	JSON     string
	FullText string
}) []byte {
	var b strings.Builder

	b.WriteString("# Meeting Summary\n\n")

	// Parse JSON into structured output
	var sm models.SummaryJSON
	if err := json.Unmarshal([]byte(summary.JSON), &sm); err == nil {
		b.WriteString(fmt.Sprintf("## Summary\n\n%s\n\n", sm.Summary))
		if len(sm.Decisions) > 0 {
			b.WriteString("## Decisions\n\n")
			for _, d := range sm.Decisions {
				b.WriteString(fmt.Sprintf("- %s\n", d))
			}
			b.WriteString("\n")
		}
		if len(sm.Tasks) > 0 {
			b.WriteString("## Tasks\n\n")
			for _, t := range sm.Tasks {
				text := t.Text
				if t.AssignedTo != nil {
					text += fmt.Sprintf(" — **%s**", *t.AssignedTo)
				}
				if t.DueDate != nil {
					text += fmt.Sprintf(" (due: %s)", *t.DueDate)
				}
				b.WriteString(fmt.Sprintf("- %s\n", text))
			}
			b.WriteString("\n")
		}
		if len(sm.Risks) > 0 {
			b.WriteString("## Risks\n\n")
			for _, r := range sm.Risks {
				b.WriteString(fmt.Sprintf("- %s\n", r))
			}
			b.WriteString("\n")
		}
		if len(sm.OpenQuestions) > 0 {
			b.WriteString("## Open Questions\n\n")
			for _, q := range sm.OpenQuestions {
				b.WriteString(fmt.Sprintf("- %s\n", q))
			}
			b.WriteString("\n")
		}
	}

	if summary.FullText != "" {
		b.WriteString("## Full Transcript\n\n")
		b.WriteString(summary.FullText)
	}

	return []byte(b.String())
}

// HealthCheck is used by Docker healthcheck and UI status panel.
func (h *Handler) HealthCheck(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// --- Helpers ---

func parseID(s string) (int64, error) {
	var id int64
	_, err := fmt.Sscanf(s, "%d", &id)
	return id, err
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func strPtr(s string) *string { return &s }

func writeJSONError(w http.ResponseWriter, msg string, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

// ParseSummary разбирает ответ LLM как JSON-протокол встречи.
// Ответ модели считается недоверенным: лишний текст и markdown-обёртки отбрасываются,
// отсутствующие поля заменяются безопасными значениями по умолчанию.
func ParseSummary(raw string) (models.SummaryJSON, error) {
	cleaned := extractJSONObject(raw)
	if cleaned == "" {
		return models.SummaryJSON{}, fmt.Errorf("no JSON object found in response")
	}

	var summary models.SummaryJSON
	if err := json.Unmarshal([]byte(cleaned), &summary); err != nil {
		return models.SummaryJSON{}, err
	}

	applySummaryDefaults(&summary)
	return summary, nil
}

// extractJSONObject вытаскивает первый сбалансированный JSON-объект из произвольного текста.
func extractJSONObject(raw string) string {
	s := strings.TrimSpace(raw)

	// Снимаем markdown-обёртку ```json ... ```
	if strings.HasPrefix(s, "```") {
		if nl := strings.IndexByte(s, '\n'); nl >= 0 {
			s = s[nl+1:]
		}
		if end := strings.LastIndex(s, "```"); end >= 0 {
			s = s[:end]
		}
		s = strings.TrimSpace(s)
	}

	start := strings.IndexByte(s, '{')
	if start < 0 {
		return ""
	}

	depth := 0
	inString := false
	escaped := false
	for i := start; i < len(s); i++ {
		c := s[i]
		if inString {
			switch {
			case escaped:
				escaped = false
			case c == '\\':
				escaped = true
			case c == '"':
				inString = false
			}
			continue
		}
		switch c {
		case '"':
			inString = true
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return s[start : i+1]
			}
		}
	}
	return ""
}

// applySummaryDefaults гарантирует, что пустые поля не превратятся в null в JSON-выводе.
func applySummaryDefaults(s *models.SummaryJSON) {
	if strings.TrimSpace(s.Summary) == "" {
		s.Summary = "Краткое содержание не сформировано."
	}
	if s.Decisions == nil {
		s.Decisions = []string{}
	}
	if s.Tasks == nil {
		s.Tasks = []models.TaskItem{}
	}
	if s.Risks == nil {
		s.Risks = []string{}
	}
	if s.OpenQuestions == nil {
		s.OpenQuestions = []string{}
	}
	if s.Quotes == nil {
		s.Quotes = []models.QuoteItem{}
	}
}

// sanitizeFileName removes dangerous characters and prevents path traversal.
var sanitizeRegex = regexp.MustCompile(`[^a-zA-Z0-9_\-. ]`)

func sanitizeFileName(name string) string {
	safe := sanitizeRegex.ReplaceAllString(name, "_")
	safe = strings.ReplaceAll(safe, "..", "")
	if safe == "" || safe == "_" {
		safe = "uploaded_file"
	}
	return safe
}
