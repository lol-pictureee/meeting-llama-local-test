package models

import (
	"encoding/json"
	"testing"
)

func TestSummaryJSONUnmarshal(t *testing.T) {
	input := `{
		"summary": "Обсуждение плана на квартал",
		"decisions": ["Утвердить бюджет"],
		"tasks": [
			{"text": "Подготовить отчёт", "assigned_to": "Иванов", "due_date": "2025-07-15"}
		],
		"risks": ["Задержка поставки"],
		"open_questions": ["Когда начнётся этап 2?"],
		"quotes": [{"text": "Нужно уложиться в срок", "start_time": 45.2, "end_time": 48.5}]
	}`

	var summary SummaryJSON
	if err := json.Unmarshal([]byte(input), &summary); err != nil {
		t.Fatalf("Failed to unmarshal: %v", err)
	}

	if summary.Summary != "Обсуждение плана на квартал" {
		t.Errorf("Expected 'Обсуждение плана на квартал', got '%s'", summary.Summary)
	}

	if len(summary.Decisions) != 1 || summary.Decisions[0] != "Утвердить бюджет" {
		t.Errorf("Unexpected decisions: %+v", summary.Decisions)
	}

	if len(summary.Tasks) != 1 {
		t.Fatalf("Expected 1 task, got %d", len(summary.Tasks))
	}
	if summary.Tasks[0].AssignedTo == nil || *summary.Tasks[0].AssignedTo != "Иванов" {
		t.Errorf("Expected assigned_to 'Иванов', got %+v", summary.Tasks[0])
	}

	if len(summary.Risks) != 1 || summary.Risks[0] != "Задержка поставки" {
		t.Errorf("Unexpected risks: %+v", summary.Risks)
	}
}

func TestSummaryJSONDefaults(t *testing.T) {
	// Test that empty/null fields produce zero values or empty slices
	input := `{"summary":"Test"}`

	var summary SummaryJSON
	if err := json.Unmarshal([]byte(input), &summary); err != nil {
		t.Fatalf("Failed to unmarshal: %v", err)
	}

	if summary.Summary != "Test" {
		t.Errorf("Expected 'Test', got '%s'", summary.Summary)
	}

	if summary.Decisions != nil {
		t.Errorf("Expected nil decisions, got %+v", summary.Decisions)
	}
	if summary.Tasks != nil {
		t.Errorf("Expected nil tasks, got %+v", summary.Tasks)
	}
}

func TestTranscriptSegmentMarshaling(t *testing.T) {
	seg := TranscriptSegment{
		StartTime: 10.5,
		EndTime:   15.3,
		Text:      "Hello world",
		Speaker:   strPtr("Alice"),
	}

	data, err := json.Marshal(seg)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	var restored TranscriptSegment
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if restored.StartTime != 10.5 || restored.EndTime != 15.3 || restored.Text != "Hello world" {
		t.Errorf("Restored segment mismatch: %+v", restored)
	}
	if restored.Speaker == nil || *restored.Speaker != "Alice" {
		t.Errorf("Expected speaker 'Alice', got %+v", restored.Speaker)
	}
}

func strPtr(s string) *string { return &s }
