# 📝 Local AI Meeting Summarizer

Локальный сервис расшифровки и протоколирования встреч. Загружаешь аудио — получаешь транскрипт с таймкодами и структурированный протокол.

**Всё выполняется локально.** Никаких облачных API, ключей и отправки файлов наружу.

---

## Стек

| Компонент | Роль | Технологии |
|-----------|------|------------|
| **web** | HTTP API + веб-интерфейс, статусы, экспорт | Go 1.24, `net/http`, gorilla/mux, `modernc.org/sqlite` |
| **whisper-service** | Распознавание речи (аудио → текст с таймкодами) | Python 3.12, FastAPI, `faster-whisper`, PyAV |
| **ollama-service** | Анализ текста (транскрипт → JSON-протокол) | Ollama, модель `gemma4:12b` |
| **SQLite** | Встречи, сегменты транскрипта, протоколы | `modernc.org/sqlite`, том `db-data` |

Схема потока данных:

```
Аудио → web (сохраняет файл, ставит статус)
          ↓
     whisper-service   (локально: речь → текст + таймкоды)
          ↓
     ollama-service    (локально: gemma4:12b → JSON-протокол)
          ↓
     SQLite + веб-интерфейс (просмотр и экспорт)
```

---

## ⚠️ Требования к памяти (важно)

Модель `gemma4:12b` занимает **~8 ГБ** в памяти, плюс нужен KV-кэш контекста.

Docker Desktop на Windows работает через WSL2, который по умолчанию берёт **только 50% RAM**. При 16 ГБ физической памяти Docker видит ~7.7 ГБ — модель **не загрузится** (ошибка `Cannot allocate memory`).

Создайте (или дополните) файл `C:\Users\<пользователь>\.wslconfig`:

```ini
[wsl2]
memory=12GB
swap=8GB
processors=6
```

Затем примените настройку:

```powershell
wsl --shutdown
# перезапустите Docker Desktop
```

Проверка, что Docker видит достаточно памяти:

```powershell
docker info --format '{{.MemTotal}}'
# должно быть больше 8 ГБ в байтах
```

Итого для комфортной работы: **16 ГБ RAM** на машине (12 ГБ отдаётся WSL), ~15 ГБ свободного диска.

---

## Запуск

### 1. Настройка окружения

```bash
cd meetings
cp .env.example .env
```

Значения по умолчанию подходят для типичной установки. Порт **11435** выбран специально, чтобы **не конфликтовать с локальным Ollama** на 11434.

### 2. Модели Ollama

Контейнер использует каталог `./ollama-models-local` как хранилище моделей (`/root/.ollama/models`).

**Если у вас уже есть модель в локальном Ollama** — скопируйте хранилище, чтобы не качать 8 ГБ заново:

```powershell
robocopy "$env:USERPROFILE\.ollama\models" ".\ollama-models-local" /E /MT:16
```

**Если модели нет** — скачайте её внутрь контейнера (нужен интернет один раз):

```bash
docker compose up -d ollama-service
docker exec meetings-ollama ollama pull gemma4:12b
```

Проверка:

```bash
docker exec meetings-ollama ollama list
```

### 3. Сборка и запуск всего стека

```bash
docker compose up -d --build
```

Первый запуск `whisper-service` скачивает веса Whisper (~142 МБ для `base`) — тоже один раз, дальше они лежат в томе `whisper-cache`.

### 4. Проверка

```bash
curl http://localhost:8080/api/health      # {"status":"ok"}
curl http://localhost:8080/api/meetings    # список встреч

docker exec meetings-whisper-service-1 python -c "import urllib.request;print(urllib.request.urlopen('http://localhost:8081/health').read())"
docker exec meetings-ollama ollama list
```

Откройте **http://localhost:8080** и загрузите аудиофайл.

---

## Проверка работы без интернета

1. Убедитесь, что модель Ollama и веса Whisper уже загружены (шаги 2–3).
2. Отключите интернет.
3. Перезапустите стек:

```bash
docker compose down
docker compose up -d
```

4. Загрузите аудио на http://localhost:8080 — транскрипция и анализ должны отработать полностью офлайн.

---

## Возможности

**Загрузка**
- Форматы: WAV, MP3, M4A, MP4, WebM (плюс OGG/FLAC на стороне Whisper)
- Ограничение размера — 200 МБ (`MAX_UPLOAD_SIZE_MB`)
- Безопасная обработка имён файлов (защита от path traversal)

**Транскрипция**
- Локальный `faster-whisper`, CPU-режим `int8`
- Таймкоды по каждому сегменту
- VAD-фильтр убирает тишину
- Автоопределение языка (`WHISPER_LANGUAGE=` пусто) или фиксация, например `ru`

**Анализ (gemma4:12b)**
- `format: json` — Ollama гарантирует синтаксически валидный JSON
- Краткое содержание, решения, задачи, риски, открытые вопросы, цитаты с таймкодами
- Ответ модели считается **недоверенным**: JSON проверяется, markdown-обёртки и текст вокруг отбрасываются, отсутствующие поля заменяются пустыми массивами
- Модель не выдумывает имена, сроки и ответственных — при отсутствии данных `null` или `[]`

**Статусы**
`loaded` → `transcribing` → `analyzing` → `completed` / `error`

Обработка идёт в фоне с собственным контекстом (не блокирует HTTP-ответ). Веб-интерфейс опрашивает статус каждые 3 секунды.

**Повторная обработка**
Кнопка «🔁 Обработать» для встреч в статусе `error` или `loaded`:

```bash
curl -X POST http://localhost:8080/api/meetings/5/reprocess
```

**Экспорт**
- Markdown: `GET /api/meetings/{id}/export?format=markdown`
- JSON: `GET /api/meetings/{id}/export?format=json`

---

## Фактические замеры (CPU, без GPU)

Замерено на этой машине (AMD/Intel CPU, 12 ГБ выделено WSL2), файл WAV 19 МБ:

| Этап | Время |
|------|-------|
| Транскрипция (Whisper `base`, int8) | ~18 секунд |
| Первая загрузка модели gemma4:12b в память | ~2–3 минуты |
| Анализ транскрипта (1567 символов) | ~5 минут |
| **Итого первый запрос** | **~8 минут** |
| **Итого последующие** (модель в памяти) | **~1–2 минуты** |

Конкретные цифры зависят от CPU, длины записи и модели Whisper. Высокую скорость никто не гарантирует — это цена локального выполнения без облака.

Уменьшить время анализа: `OLLAMA_KEEP_ALIVE=30m` уже задан в compose — модель остаётся в памяти 30 минут между запросами.

---

## Настройки (.env)

| Переменная | По умолчанию | Описание |
|------------|--------------|----------|
| `GO_BACKEND_PORT` | 8080 | Порт веб-интерфейса и API |
| `WHISPER_MODEL` | base | `tiny` / `base` / `small` / `medium` / `large-v3` |
| `WHISPER_COMPUTE_TYPE` | int8 | `int8` быстрее на CPU, `float32` точнее |
| `WHISPER_LANGUAGE` | *(пусто)* | Пусто = автоопределение, иначе `ru` |
| `WHISPER_BEAM_SIZE` | 5 | Больше = точнее, но медленнее |
| `OLLAMA_MODEL` | gemma4:12b | Имя модели в Ollama |
| `OLLAMA_HOST_PORT` | 11435 | Порт Ollama на хосте (11434 занят локальным Ollama) |
| `MAX_UPLOAD_SIZE_MB` | 200 | Лимит размера загрузки |
| `AUDIO_TIMEOUT_SECONDS` | 1800 | Таймаут транскрипции |
| `ANALYSIS_TIMEOUT_SECONDS` | 900 | Таймаут анализа LLM |

---

## Данные и тома

```bash
docker volume ls | Select-String meetings
```

- `meetings_uploads` — загруженные аудиофайлы
- `meetings_db-data` — SQLite (`meetings.db`)
- `meetings_whisper-cache` — веса Whisper (скачиваются один раз)
- `meetings_exports` — каталог экспорта
- `./ollama-models-local` — модели Ollama (bind-mount на хосте)

Полное удаление данных:

```bash
docker compose down -v
```

---

## Тесты

```bash
cd backend-go
go test ./... -v
```

Покрыто:
- разбор ответа LLM: чистый JSON, markdown-обёртка ` ```json `, текст вокруг JSON, фигурные скобки внутри строк
- невалидные ответы: пустой, обрывок, массив вместо объекта — понятная ошибка вместо паники
- заполнение отсутствующих полей пустыми массивами (никаких `null` в выводе)
- синтетическая фикстура `handlers/testdata/llm_response.json` — проверка анализа без запуска Whisper
- санитизация имён файлов (path traversal)
- API: health-check, пустой список встреч, экспорт в Markdown

---

## Структура проекта

```
meetings/
├── .env.example                 # шаблон настроек
├── docker-compose.yml           # web + whisper-service + ollama-service
├── ollama-models-local/         # модели Ollama (bind-mount)
├── backend-go/
│   ├── Dockerfile
│   ├── main.go                  # запуск, graceful shutdown
│   ├── models/models.go         # Meeting, TranscriptSegment, SummaryJSON
│   ├── store/sqlite.go          # CRUD, транскрипт с таймкодами
│   ├── client/whisper.go        # HTTP-клиент Whisper
│   ├── client/ollama.go         # HTTP-клиент Ollama (format=json)
│   ├── handlers/handlers.go     # пайплайн, API, парсер LLM
│   ├── handlers/routes.go       # маршруты, статика
│   ├── handlers/summary_test.go # тесты парсера и санитизации
│   ├── handlers/testdata/       # синтетическая фикстура LLM
│   └── frontend-static/         # index.html, style.css, app.js
└── backend-python/
    ├── Dockerfile
    ├── requirements.txt         # fastapi, faster-whisper, av==12.3.0
    └── app.py                   # /health, /transcribe
```

---

## Известные ограничения

1. **Без диаризации** — спикеры не разделяются, поле `speaker` всегда пустое.
2. **Одна модель Whisper за раз** — при смене `WHISPER_MODEL` нужна пересборка/перезапуск сервиса и повторная загрузка весов.
3. **Анализ на CPU медленный** — см. замеры выше. GPU не используется.
4. **Whisper `base` не идеален** — на плохом звуке возможны ошибки распознавания. Для точности: `WHISPER_MODEL=small` или `medium` (больше RAM и времени).
5. **Обрезание длинных транскриптов** — в промт уходит до 24 000 символов. Очень длинные встречи анализируются частично.
6. **Порт 11435, а не 11434** — сделано намеренно, чтобы не конфликтовать с локальным Ollama. Если локального нет, можно сменить `OLLAMA_HOST_PORT` на 11434.

---

## Устранение неполадок

| Симптом | Причина | Решение |
|---------|---------|---------|
| `Cannot allocate memory` | WSL2 видит < 8 ГБ | Поднять `memory=12GB` в `.wslconfig`, выполнить `wsl --shutdown` |
| `model 'gemma4:12b' not found` | Модели нет в контейнере Ollama | `docker exec meetings-ollama ollama pull gemma4:12b` или скопировать `~/.ollama/models` в `./ollama-models-local` |
| `ports are not available: 11434` | Занят локальным Ollama | Оставить `OLLAMA_HOST_PORT=11435` |
| `metadata_errors` в логах Whisper | Старый PyAV | В `requirements.txt` зафиксирован `av==12.3.0` |
| Встреча висит в `loaded` | Ошибка в фоне — смотрите логи | `docker logs meetings-web-1` и кнопка «🔁 Обработать» |
| Транскрипт пустой | Тишина или неверный формат | Проверить файл, попробовать `WHISPER_MODEL=small` |

Просмотр логов:

```bash
docker logs -f meetings-web-1
docker logs -f meetings-whisper-service-1
docker logs -f meetings-ollama
```