package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"
)

// OllamaClient talks to a local Ollama instance serving gemma4:12b.
type OllamaClient struct {
	baseURL string
	http    *http.Client
	model   string
}

// NewOllamaClient creates an OllamaClient from env vars OLLAMA_HOST, OLLAMA_PORT, OLLAMA_MODEL.
func NewOllamaClient() *OllamaClient {
	host := os.Getenv("OLLAMA_HOST")
	if host == "" {
		host = "localhost"
	}
	port := os.Getenv("OLLAMA_PORT")
	if port == "" {
		port = "11434"
	}
	model := os.Getenv("OLLAMA_MODEL")
	if model == "" {
		model = "gemma4:12b"
	}
	timeoutSec := checkInt("ANALYSIS_TIMEOUT_SECONDS", 300)
	return &OllamaClient{
		baseURL: fmt.Sprintf("http://%s:%s", host, port),
		model:   model,
		http: &http.Client{
			Timeout: time.Duration(timeoutSec) * time.Second,
		},
	}
}

// GenerateSummary sends the transcription text to the LLM and expects a JSON summary back.
func (c *OllamaClient) GenerateSummary(ctx context.Context, fullText string) (string, error) {
	prompt := buildSummarizePrompt(fullText)

	reqBody := map[string]interface{}{
		"model":  c.model,
		"prompt": prompt,
		"stream": false,
		// format=json заставляет Ollama генерировать синтаксически валидный JSON
		"format": "json",
		"options": map[string]interface{}{
			"temperature": 0.1,
			"num_predict": 4096,
			// 4096 с запасом хватает на протокол встречи и экономит память под KV-кэш.
			// Большой num_ctx резко увеличивает потребление RAM при загрузке модели.
			"num_ctx": 4096,
		},
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("marshal ollama request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/api/generate", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("create ollama request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("ollama generate failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		buf := new(bytes.Buffer)
		buf.ReadFrom(resp.Body)
		return "", fmt.Errorf("ollama returned status %d: %s", resp.StatusCode, buf.String())
	}

	var result struct {
		Response string `json:"response"`
		Done     bool   `json:"done"`
		Error    string `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("decode ollama response: %w", err)
	}
	if result.Error != "" {
		return "", fmt.Errorf("ollama error: %s", result.Error)
	}
	if !result.Done {
		return "", fmt.Errorf("ollama generation not complete")
	}

	return result.Response, nil
}

// HealthCheck verifies that the Ollama service is reachable and the model is loaded.
func (c *OllamaClient) HealthCheck(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, "GET", c.baseURL+"/api/tags", nil)
	if err != nil {
		return err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("ollama health check returned status %d", resp.StatusCode)
	}

	var result struct {
		Models []struct {
			Name string `json:"name"`
		} `json:"models"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return err
	}

	found := false
	for _, m := range result.Models {
		if m.Name == c.model || m.Name == c.model+":latest" {
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("model %s not found in Ollama models list", c.model)
	}
	return nil
}

func buildSummarizePrompt(fullText string) string {
	return fmt.Sprintf(`Ты — ассистент, который составляет протокол встречи. Проанализируй транскрипт и верни ОДИН JSON-объект строго такой структуры:

{
  "summary": "краткое содержание встречи на русском (3-6 предложений)",
  "decisions": ["принятое решение"],
  "tasks": [
    {"text": "что нужно сделать", "assigned_to": "имя или null", "due_date": "срок или null"}
  ],
  "risks": ["риск или проблема"],
  "open_questions": ["вопрос, оставшийся без ответа"],
  "quotes": [
    {"text": "дословная цитата из транскрипта", "start_time": 12.5, "end_time": 15.0}
  ]
}

Жёсткие правила:
1. НЕ выдумывай факты, имена, даты, сроки и ответственных. Если в транскрипте этого нет — ставь null или пустой массив [].
2. В "tasks" попадают только явно прозвучавшие поручения («сделай», «подготовь», «я беру на себя»).
3. В "quotes" — только дословные фразы из транскрипта. Если подходящих цитат нет, верни [].
4. Для цитат указывай start_time и end_time из таймкодов транскрипта, если они там есть.
5. Отвечай ТОЛЬКО JSON-объектом. Без markdown, без пояснений до или после.
6. Все текстовые поля — на русском языке.

ТРАНСКРИПТ (формат строк: [начало — конец] текст):
%s`, truncateText(fullText, 24000))
}

func truncateText(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "\n... [транскрипт обрезан]"
}
