package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"meeting-summarizer/models"
	"meeting-summarizer/store"
)

func setupTestHandler(t *testing.T) (*Handler, func()) {
	t.Helper()

	// Create temp database file in temp dir
	dbDir := t.TempDir()
	dbPath := dbDir + "/test.db"

	s, err := store.New(dbPath)
	if err != nil {
		t.Fatalf("New store failed: %v", err)
	}
	ctx := context.Background()
	if err := s.Open(ctx); err != nil {
		t.Fatalf("Open store failed: %v", err)
	}

	h := NewHandler(s)

	cleanup := func() {
		s.Close()
	}

	return h, cleanup
}

func TestHealthCheck(t *testing.T) {
	h, cleanup := setupTestHandler(t)
	defer cleanup()

	req := httptest.NewRequest("GET", "/api/health", nil)
	w := httptest.NewRecorder()

	h.HealthCheck(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected 200, got %d", resp.StatusCode)
	}

	var body map[string]string
	json.NewDecoder(resp.Body).Decode(&body)
	if body["status"] != "ok" {
		t.Errorf("Expected status 'ok', got '%s'", body["status"])
	}
}

func TestListMeetingsEmpty(t *testing.T) {
	h, cleanup := setupTestHandler(t)
	defer cleanup()

	req := httptest.NewRequest("GET", "/api/meetings", nil)
	w := httptest.NewRecorder()

	h.ListMeetings(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected 200, got %d", resp.StatusCode)
	}

	var meetings []models.Meeting
	json.NewDecoder(resp.Body).Decode(&meetings)
	if len(meetings) != 0 {
		t.Errorf("Expected empty list, got %d items", len(meetings))
	}
}

func TestSummaryMarkdownExport(t *testing.T) {
	h, cleanup := setupTestHandler(t)
	defer cleanup()

	summaryData := `{
		"summary": "Test summary",
		"decisions": ["Decision 1"],
		"tasks": [{"text": "Task A", "assigned_to": "Bob", "due_date": "2025-12-31"}],
		"risks": [],
		"open_questions": ["Question X?"],
		"quotes": []
	}`

	result := &struct {
		JSON     string
		FullText string
	}{
		JSON:     summaryData,
		FullText: "Transcript text here",
	}

	md := h.formatMarkdown(result)
	mdStr := string(md)

	expectedParts := []string{
		"# Meeting Summary",
		"## Summary",
		"Test summary",
		"## Decisions",
		"Decision 1",
		"## Tasks",
		"Task A",
		"**Bob**",
		"## Open Questions",
		"Question X?",
		"## Full Transcript",
		"Transcript text here",
	}

	for _, part := range expectedParts {
		if !containsSubstring(mdStr, part) {
			t.Errorf("Expected markdown to contain '%s'\nGot:\n%s", part, mdStr)
		}
	}
}

func containsSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
