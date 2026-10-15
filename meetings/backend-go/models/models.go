package models

import "time"

// MeetingStatus describes the processing lifecycle of a meeting file.
type MeetingStatus string

const (
	StatusLoaded      MeetingStatus = "loaded"
	StatusTranscribing MeetingStatus = "transcribing"
	StatusAnalyzing   MeetingStatus = "analyzing"
	StatusCompleted   MeetingStatus = "completed"
	StatusError       MeetingStatus = "error"
)

// Meeting represents an uploaded audio file and its current state.
type Meeting struct {
	ID            int64          `json:"id"`
	Filename      string         `json:"filename"`
	OriginalName  string         `json:"original_name"`
	SizeBytes     int64          `json:"size_bytes"`
	Status        MeetingStatus  `json:"status"`
	ErrorMsg      *string        `json:"error_message,omitempty"`
	CreatedAt     time.Time      `json:"created_at"`
	UpdatedAt     time.Time      `json:"updated_at"`
}

// TranscriptSegment is one chunk of transcription with timestamps.
type TranscriptSegment struct {
	StartTime float64 `json:"start_time"`
	EndTime   float64 `json:"end_time"`
	Text      string  `json:"text"`
	Speaker   *string `json:"speaker,omitempty"`
}

// MeetingWithTranscript holds a meeting record together with its transcript.
type MeetingWithTranscript struct {
	Meeting   Meeting           `json:"meeting"`
	Segments  []TranscriptSegment `json:"segments"`
	FullText  string            `json:"full_text"`
}

// SummaryJSON is the structure expected from Gemma summarization.
type SummaryJSON struct {
	Summary      string         `json:"summary"`
	Decisions    []string       `json:"decisions"`
	Tasks        []TaskItem     `json:"tasks"`
	Risks        []string       `json:"risks"`
	OpenQuestions []string     `json:"open_questions"`
	Quotes       []QuoteItem    `json:"quotes"`
}

// TaskItem represents a concrete task mentioned in the meeting.
type TaskItem struct {
	Text       string  `json:"text"`
	AssignedTo *string `json:"assigned_to"`
	DueDate    *string `json:"due_date"`
}

// QuoteItem captures a verbatim quote with optional timing.
type QuoteItem struct {
	Text       string  `json:"text"`
	StartTime  *float64 `json:"start_time,omitempty"`
	EndTime    *float64 `json:"end_time,omitempty"`
}

// ExportFormat specifies how to serialize the summary for download.
type ExportFormat string

const (
	ExportMarkdown ExportFormat = "markdown"
	ExportJSON     ExportFormat = "json"
)
