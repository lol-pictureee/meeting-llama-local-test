package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"meeting-summarizer/models"

	_ "modernc.org/sqlite"
)

// Store wraps all database operations for meetings, transcripts, and summaries.
type Store struct {
	db     *sql.DB
	dbPath string
}

// New creates a new Store backed by SQLite at dbPath.
func New(dbPath string) (*Store, error) {
	return &Store{dbPath: dbPath}, nil
}

// Open opens the SQLite connection and runs migrations.
func (s *Store) Open(ctx context.Context) error {
	db, err := sql.Open("sqlite", s.dbPath)
	if err != nil {
		return fmt.Errorf("open sqlite: %w", err)
	}
	s.db = db
	return s.migrate(ctx)
}

// Close closes the SQLite connection.
func (s *Store) Close() error {
	if s.db != nil {
		return s.db.Close()
	}
	return nil
}

func (s *Store) migrate(ctx context.Context) error {
	query := `
	CREATE TABLE IF NOT EXISTS meetings (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		filename TEXT NOT NULL UNIQUE,
		original_name TEXT NOT NULL,
		size_bytes INTEGER NOT NULL,
		status TEXT NOT NULL DEFAULT 'loaded',
		error_message TEXT,
		created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
		updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS transcript_segments (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		meeting_id INTEGER NOT NULL,
		start_time REAL NOT NULL,
		end_time REAL NOT NULL,
		text TEXT NOT NULL,
		speaker TEXT,
		FOREIGN KEY (meeting_id) REFERENCES meetings(id) ON DELETE CASCADE
	);

	CREATE TABLE IF NOT EXISTS meeting_summary (
		meeting_id INTEGER PRIMARY KEY,
		summary_json TEXT,
		full_text TEXT,
		created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY (meeting_id) REFERENCES meetings(id) ON DELETE CASCADE
	);

	CREATE INDEX IF NOT EXISTS idx_meetings_status ON meetings(status);
	CREATE INDEX IF NOT EXISTS idx_segments_meeting ON transcript_segments(meeting_id);
	`
	_, err := s.db.ExecContext(ctx, query)
	return err
}

// CreateMeeting inserts a new meeting record. Returns the created ID.
func (s *Store) CreateMeeting(ctx context.Context, m *models.Meeting) error {
	now := time.Now().UTC().Format(time.RFC3339)
	stmt := `INSERT INTO meetings (filename, original_name, size_bytes, status, error_message, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?)`
	res, err := s.db.ExecContext(ctx, stmt, m.Filename, m.OriginalName, m.SizeBytes, string(m.Status), ptrVal(m.ErrorMsg), now, now)
	if err != nil {
		return err
	}
	id, _ := res.LastInsertId()
	m.ID = id
	m.CreatedAt, _ = time.Parse(time.RFC3339, now)
	m.UpdatedAt = m.CreatedAt
	return nil
}

// GetMeeting retrieves a meeting by its ID.
func (s *Store) GetMeeting(ctx context.Context, id int64) (*models.Meeting, error) {
	row := s.db.QueryRowContext(ctx, `SELECT id, filename, original_name, size_bytes, status, error_message, created_at, updated_at FROM meetings WHERE id = ?`, id)
	var m models.Meeting
	var errMsg *string
	var createdAt, updatedAt string
	err := row.Scan(&m.ID, &m.Filename, &m.OriginalName, &m.SizeBytes, (*string)(&m.Status), &errMsg, &createdAt, &updatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("meeting %d not found", id)
		}
		return nil, err
	}
	if errMsg != nil {
		m.ErrorMsg = errMsg
	}
	m.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	m.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
	return &m, nil
}

// UpdateMeetingStatus updates only the status and optionally an error message.
func (s *Store) UpdateMeetingStatus(ctx context.Context, id int64, status models.MeetingStatus, errMsg *string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := s.db.ExecContext(ctx, `UPDATE meetings SET status = ?, error_message = ?, updated_at = ? WHERE id = ?`, string(status), ptrVal(errMsg), now, id)
	return err
}

// SaveTranscriptSegments replaces all segments for a given meeting.
func (s *Store) SaveTranscriptSegments(ctx context.Context, meetingID int64, segments []models.TranscriptSegment) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(ctx, `DELETE FROM transcript_segments WHERE meeting_id = ?`, meetingID)
	if err != nil {
		return err
	}
	stmt, err := tx.PrepareContext(ctx, `INSERT INTO transcript_segments (meeting_id, start_time, end_time, text, speaker) VALUES (?, ?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for _, seg := range segments {
		speaker := ptrVal(seg.Speaker)
		_, err = stmt.ExecContext(ctx, meetingID, seg.StartTime, seg.EndTime, seg.Text, speaker)
		if err != nil {
			return err
		}
	}
	return tx.Commit()
}

// GetTranscriptSegments retrieves all segments for a given meeting, ordered by start_time.
func (s *Store) GetTranscriptSegments(ctx context.Context, meetingID int64) ([]models.TranscriptSegment, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT start_time, end_time, text, speaker FROM transcript_segments WHERE meeting_id = ? ORDER BY start_time ASC`, meetingID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var segments []models.TranscriptSegment
	for rows.Next() {
		var seg models.TranscriptSegment
		var speaker *string
		err := rows.Scan(&seg.StartTime, &seg.EndTime, &seg.Text, &speaker)
		if err != nil {
			return nil, err
		}
		seg.Speaker = speaker
		segments = append(segments, seg)
	}
	return segments, rows.Err()
}

// FullText возвращает транскрипт с таймкодами: строки вида "[12.5 — 15.0] текст".
// Таймкоды нужны модели, чтобы ссылаться на моменты встречи в цитатах.
func (s *Store) FullText(ctx context.Context, meetingID int64) (string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT start_time, end_time, text, speaker FROM transcript_segments WHERE meeting_id = ? ORDER BY start_time ASC`, meetingID)
	if err != nil {
		return "", err
	}
	defer rows.Close()

	var b strings.Builder
	for rows.Next() {
		var start, end float64
		var text string
		var speaker *string
		if err := rows.Scan(&start, &end, &text, &speaker); err != nil {
			return "", err
		}
		if speaker != nil && *speaker != "" {
			fmt.Fprintf(&b, "[%.1f — %.1f] %s: %s\n", start, end, *speaker, text)
		} else {
			fmt.Fprintf(&b, "[%.1f — %.1f] %s\n", start, end, text)
		}
	}
	return b.String(), rows.Err()
}

// PlainText возвращает транскрипт без таймкодов (для экспорта).
func (s *Store) PlainText(ctx context.Context, meetingID int64) (string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT text FROM transcript_segments WHERE meeting_id = ? ORDER BY start_time ASC`, meetingID)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	var parts []string
	for rows.Next() {
		var t string
		if err := rows.Scan(&t); err != nil {
			return "", err
		}
		parts = append(parts, t)
	}
	return strings.Join(parts, "\n"), rows.Err()
}

// SaveSummary stores the generated summary for a meeting.
func (s *Store) SaveSummary(ctx context.Context, meetingID int64, summaryJSON, fullText string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := s.db.ExecContext(ctx, `INSERT OR REPLACE INTO meeting_summary (meeting_id, summary_json, full_text, created_at) VALUES (?, ?, ?, ?)`, meetingID, summaryJSON, fullText, now)
	return err
}

// GetSummary retrieves the summary for a meeting if available.
func (s *Store) GetSummary(ctx context.Context, meetingID int64) (*struct {
	JSON     string
	FullText string
}, error) {
	row := s.db.QueryRowContext(ctx, `SELECT summary_json, full_text FROM meeting_summary WHERE meeting_id = ?`, meetingID)
	var r struct {
		JSON     string
		FullText string
	}
	err := row.Scan(&r.JSON, &r.FullText)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil // no summary yet
		}
		return nil, err
	}
	return &r, nil
}

// DeleteSummary удаляет сохранённый протокол встречи (перед повторной обработкой).
func (s *Store) DeleteSummary(ctx context.Context, meetingID int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM meeting_summary WHERE meeting_id = ?`, meetingID)
	return err
}

// DeleteTranscriptSegments удаляет сегменты транскрипта встречи.
func (s *Store) DeleteTranscriptSegments(ctx context.Context, meetingID int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM transcript_segments WHERE meeting_id = ?`, meetingID)
	return err
}

// ListMeetings returns up to limit meetings ordered by creation date descending.
func (s *Store) ListMeetings(ctx context.Context, limit int) ([]models.Meeting, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id, filename, original_name, size_bytes, status, error_message, created_at, updated_at FROM meetings ORDER BY created_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var meetings []models.Meeting
	for rows.Next() {
		var m models.Meeting
		var errMsg *string
		var createdAt, updatedAt string
		err := rows.Scan(&m.ID, &m.Filename, &m.OriginalName, &m.SizeBytes, (*string)(&m.Status), &errMsg, &createdAt, &updatedAt)
		if err != nil {
			return nil, err
		}
		m.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
		m.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
		if errMsg != nil {
			m.ErrorMsg = errMsg
		}
		meetings = append(meetings, m)
	}
	return meetings, rows.Err()
}

// Helper functions
func ptrVal(v interface{}) interface{} {
	return v // return value as-is for SQL args
}
