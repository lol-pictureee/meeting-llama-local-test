package client

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"

	"meeting-summarizer/models"
)

// WhisperClient talks to the Python faster-whisper service over HTTP.
type WhisperClient struct {
	baseURL string
	http    *http.Client
}

// NewWhisperClient creates a new WhisperClient from env vars WHISPER_HOST and WHISPER_PORT.
func NewWhisperClient() *WhisperClient {
	host := os.Getenv("WHISPER_HOST")
	if host == "" {
		host = "localhost"
	}
	port := os.Getenv("WHISPER_PORT")
	if port == "" {
		port = "8081"
	}
	timeoutSec := checkInt("AUDIO_TIMEOUT_SECONDS", 600)
	return &WhisperClient{
		baseURL: fmt.Sprintf("http://%s:%s", host, port),
		http: &http.Client{
			Timeout: time.Duration(timeoutSec) * time.Second,
		},
	}
}

func checkInt(key string, def int) int {
	val := os.Getenv(key)
	if val == "" {
		return def
	}
	i := 0
	fmt.Sscanf(val, "%d", &i)
	if i <= 0 {
		i = def
	}
	return i
}

// Transcribe sends audio bytes to the whisper service and returns segments with timestamps.
func (c *WhisperClient) Transcribe(ctx context.Context, filename string, data []byte) ([]models.TranscriptSegment, error) {
	payload := map[string]interface{}{
		"filename": filename,
		"data":     base64.StdEncoding.EncodeToString(data),
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal whisper request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/transcribe", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create whisper request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("whisper transcribe request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		buf := new(bytes.Buffer)
		buf.ReadFrom(resp.Body)
		return nil, fmt.Errorf("whisper returned status %d: %s", resp.StatusCode, buf.String())
	}

	var result struct {
		Segments []map[string]interface{} `json:"segments"`
		Error    *string                  `json:"error,omitempty"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode whisper response: %w", err)
	}
	if result.Error != nil {
		return nil, fmt.Errorf("whisper error: %s", *result.Error)
	}

	var segments []models.TranscriptSegment
	for _, segMap := range result.Segments {
		seg := models.TranscriptSegment{}
		if v, ok := segMap["start_time"]; ok {
			switch val := v.(type) {
			case float64:
				seg.StartTime = val
			case int:
				seg.StartTime = float64(val)
			}
		}
		if v, ok := segMap["end_time"]; ok {
			switch val := v.(type) {
			case float64:
				seg.EndTime = val
			case int:
				seg.EndTime = float64(val)
			}
		}
		if v, ok := segMap["text"]; ok {
			if s, ok := v.(string); ok {
				seg.Text = s
			}
		}
		if v, ok := segMap["speaker"]; ok && v != nil {
			if speaker, ok := v.(string); ok {
				seg.Speaker = &speaker
			}
		}
		segments = append(segments, seg)
	}

	return segments, nil
}
