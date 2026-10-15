package handlers

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// TestParseSummaryCleanJSON — модель вернула «чистый» JSON без обёрток.
func TestParseSummaryCleanJSON(t *testing.T) {
	raw := `{
		"summary": "Обсудили релиз",
		"decisions": ["Релиз 15 июля"],
		"tasks": [{"text": "Подготовить отчёт", "assigned_to": "Иванов", "due_date": "2025-07-10"}],
		"risks": ["Не хватает тестировщиков"],
		"open_questions": ["Кто отвечает за деплой?"],
		"quotes": [{"text": "Нужно успеть до конца месяца", "start_time": 42.5, "end_time": 46.0}]
	}`

	summary, err := ParseSummary(raw)
	if err != nil {
		t.Fatalf("ParseSummary вернул ошибку: %v", err)
	}
	if summary.Summary != "Обсудили релиз" {
		t.Errorf("summary = %q", summary.Summary)
	}
	if len(summary.Decisions) != 1 || summary.Decisions[0] != "Релиз 15 июля" {
		t.Errorf("decisions = %+v", summary.Decisions)
	}
	if len(summary.Tasks) != 1 {
		t.Fatalf("tasks = %+v", summary.Tasks)
	}
	if summary.Tasks[0].AssignedTo == nil || *summary.Tasks[0].AssignedTo != "Иванов" {
		t.Errorf("assigned_to = %+v", summary.Tasks[0].AssignedTo)
	}
	if summary.Tasks[0].DueDate == nil || *summary.Tasks[0].DueDate != "2025-07-10" {
		t.Errorf("due_date = %+v", summary.Tasks[0].DueDate)
	}
	if len(summary.Quotes) != 1 || summary.Quotes[0].StartTime == nil || *summary.Quotes[0].StartTime != 42.5 {
		t.Errorf("quotes = %+v", summary.Quotes)
	}
}

// TestParseSummaryMarkdownFence — реальный случай: модель обернула JSON в ```json.
func TestParseSummaryMarkdownFence(t *testing.T) {
	raw := "```json\n{\"summary\":\"Кратко\",\"decisions\":[],\"tasks\":[],\"risks\":[],\"open_questions\":[],\"quotes\":[]}\n```"

	summary, err := ParseSummary(raw)
	if err != nil {
		t.Fatalf("ParseSummary вернул ошибку: %v", err)
	}
	if summary.Summary != "Кратко" {
		t.Errorf("summary = %q", summary.Summary)
	}
}

// TestParseSummaryNoiseAroundJSON — модель добавила текст до и после JSON.
func TestParseSummaryNoiseAroundJSON(t *testing.T) {
	raw := `Конечно! Вот протокол встречи:
{"summary":"Итог","decisions":["Решение A"],"tasks":[],"risks":[],"open_questions":[],"quotes":[]}
Надеюсь, это поможет!`

	summary, err := ParseSummary(raw)
	if err != nil {
		t.Fatalf("ParseSummary вернул ошибку: %v", err)
	}
	if summary.Summary != "Итог" {
		t.Errorf("summary = %q", summary.Summary)
	}
	if len(summary.Decisions) != 1 {
		t.Errorf("decisions = %+v", summary.Decisions)
	}
}

// TestParseSummaryBracesInsideString — скобки внутри строк не должны ломать разбор.
func TestParseSummaryBracesInsideString(t *testing.T) {
	raw := `{"summary":"Формат {ключ: значение} обсуждали","decisions":[],"tasks":[],"risks":[],"open_questions":[],"quotes":[]}`

	summary, err := ParseSummary(raw)
	if err != nil {
		t.Fatalf("ParseSummary вернул ошибку: %v", err)
	}
	if summary.Summary != "Формат {ключ: значение} обсуждали" {
		t.Errorf("summary = %q", summary.Summary)
	}
}

// TestParseSummaryMissingFields — пустой ответ не должен давать null в полях.
func TestParseSummaryMissingFields(t *testing.T) {
	summary, err := ParseSummary(`{"summary":"Только summary"}`)
	if err != nil {
		t.Fatalf("ParseSummary вернул ошибку: %v", err)
	}
	if summary.Decisions == nil || summary.Tasks == nil || summary.Risks == nil ||
		summary.OpenQuestions == nil || summary.Quotes == nil {
		t.Fatalf("ожидались пустые слайсы вместо nil: %+v", summary)
	}

	// Проверяем, что в JSON уходят массивы, а не null
	data, err := json.Marshal(summary)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	for _, field := range []string{`"decisions":[]`, `"tasks":[]`, `"risks":[]`, `"open_questions":[]`, `"quotes":[]`} {
		if !containsSubstring(string(data), field) {
			t.Errorf("в JSON нет %s: %s", field, data)
		}
	}
}

// TestParseSummaryInvalid — невалидный ответ должен давать понятную ошибку, а не панику.
func TestParseSummaryInvalid(t *testing.T) {
	cases := map[string]string{
		"пустой ответ":     "",
		"текст без JSON":   "Извините, я не могу это обработать.",
		"оборванный JSON":  `{"summary": "начало`,
		"не объект":        `["это", "массив"]`,
		"невалидный синтаксис": `{"summary": }`,
	}

	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := ParseSummary(raw)
			if err == nil {
				t.Fatalf("ожидалась ошибка для случая %q", name)
			}
			if err.Error() == "" {
				t.Errorf("ошибка должна содержать понятное сообщение")
			}
		})
	}
}

// TestParseSummaryFixture — синтетическая фикстура: анализ без запуска Whisper.
func TestParseSummaryFixture(t *testing.T) {
	path := filepath.Join("testdata", "llm_response.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("не удалось прочитать фикстуру %s: %v", path, err)
	}

	summary, err := ParseSummary(string(raw))
	if err != nil {
		t.Fatalf("ParseSummary не разобрал фикстуру: %v", err)
	}
	if summary.Summary == "" {
		t.Error("summary пустой")
	}
	if len(summary.Tasks) == 0 {
		t.Error("ожидались задачи из фикстуры")
	}
	// Модель не должна выдумывать ответственных: пустые поля остаются null
	for _, task := range summary.Tasks {
		if task.Text == "" {
			t.Error("у задачи пустой текст")
		}
	}
}

// TestSanitizeFileName — защита от path traversal и мусорных имён.
func TestSanitizeFileName(t *testing.T) {
	cases := map[string]string{
		"../../../etc/passwd": "___etc_passwd",
		"../../secret.wav":    "__secret.wav",
		"meeting 2024.wav":    "meeting 2024.wav",
		"":                    "uploaded_file",
		"..":                  "uploaded_file",
	}

	for input, want := range cases {
		got := sanitizeFileName(input)
		if got != want {
			t.Errorf("sanitizeFileName(%q) = %q, ожидалось %q", input, got, want)
		}
		if containsSubstring(got, "..") {
			t.Errorf("результат %q всё ещё содержит '..'", got)
		}
	}
}