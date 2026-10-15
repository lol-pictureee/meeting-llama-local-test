# 📝 Local AI Meeting Summarizer

**Локальный сервис анализа встреч без облачных API.** Загружаете аудиозапись — получаете транскрипт и структурированный протокол. Всё работает на вашей машине. Никаких API-ключей. Никаких данных не покидает ваш компьютер.

## 🏗 Архитектура

```
┌─────────────┐     ┌──────────────────┐     ┌─────────────┐
│   Browser    │────▶│  Go Backend      │────▶│ Python      │
│ (localhost:8080)  │  (net/http + UI) │     │ Whisper     │
└─────────────┘     │                  │     │ (faster-    │
                    │  SQLite DB       │◀────│  whisper)   │
                    │  (meetings.db)   │     └─────────────┘
                    │                  │
                    │  Ollama Client   │────▶│ Ollama      │
                    └──────────────────┘     │ (gemma4:12b)
                                             └─────────────┘
```

### Компоненты

| Компонент | Что делает | Стек |
|-----------|------------|------|
| **Go Backend** | HTTP API, веб-интерфейс, загрузка файлов, управление жизненным циклом встреч | Go 1.24, net/http, gorilla/mux, modernc.org/sqlite |
| **Whisper Service** | Локальная транскрипция аудио в текст с таймкодами | Python 3.12, FastAPI, faster-whisper |
| **Ollama** | Локальная LLM для анализа транскрипта | gemma4:12b, ollama/ollama image |
| **SQLite** | Хранение встреч, транскриптов, результатов анализа | modernc.org/sqlite (pure Go, CGO_ENABLED=0) |

## 🚀 Быстрый старт

### Требования

- **Docker Desktop** (или Docker Engine + Docker Compose v2)
- **RAM**: минимум 8 ГБ (16 ГБ рекомендуется для gemma4:12b)
- **Диск**: ~10 ГБ для моделей (gemma4:12b ~8 ГБ, Whisper models ~1 ГБ)
- **GPU** (опционально): ускорит whisper и ollama, но всё работает на CPU

### Шаг 1. Клонирование и настройка

```bash
cd meetings
cp .env.example .env
```

Настройте переменные в `.env` при необходимости. По умолчанию всё работает из коробки.

### Шаг 2. Однократная загрузка моделей

```bash
# Сборка образов и начальная загрузка
docker compose pull

# Одиночный запуск для скачивания моделей (нужен интернет только один раз)
docker compose up --detach web whisper-service ollama-service

# Дождитесь полной загрузки моделей (~5 минут при быстром интернете)
docker compose logs -f ollama-service
```

### Шаг 3. Запуск

```bash
docker compose up --detach
```

Сервисы запустятся автоматически:
- **Веб-интерфейс**: http://localhost:8080
- **API**: http://localhost:8080/api/meetings
- **Whisper**: http://localhost:8081/health
- **Ollama**: http://localhost:11434/api/tags

### Шаг 4. Проверка работы без интернета

1. Отключите интернет
2. `docker compose down && docker compose up`
3. Сервис должен полностью функционировать

Загрузите любой аудиофайл (.wav, .mp3, .m4a, .mp4, .webm) через веб-интерфейс и дождитесь результата.

## 📊 Возможности MVP

### Загрузка файлов
- Поддержка WAV, MP3, M4A, MP4, WebM
- Автоматическая проверка расширения и размера (до 200 МБ)
- Безопасная обработка имён файлов без path traversal

### Транскрипция
- Локальное распознавание через faster-whisper
- Сегментация с таймкодами (start_time, end_time)
- Voice Activity Detection (удаление тишины)
- Поддержка русского языка по умолчанию

### Анализ встреч через Gemma 4 12B
- Краткое содержание встречи
- Ключевые решения
- Задачи с ответственным и сроком (только если явно упомянуты)
- Риски и открытые вопросы
- Цитаты с таймкодами

**Важно:** Модель не выдумывает факты. Если данные отсутствуют — возвращает пустые массивы или null.

### Статусы обработки
- `loaded` → `transcribing` → `analyzing` → `completed` (или `error`)
- Асинхронная обработка (HTTP-запрос не блокируется)
- Автотест каждые 3 секунды обновляет статус в интерфейсе

### Экспорт
- Скачать результат в JSON
- Скачать результат в Markdown
- Просмотр полного транскрипта

## 🧪 Тестирование

### Go-тесты

```bash
cd backend-go
go test ./...
```

Тестируются:
- Парсинг JSON-ответа от Gemma
- Маршалинг/демаршалинг сегментов транскрипта
- Пустой список встреч
- Форматирование экспорта в Markdown

### Health checks

Все сервисы имеют встроенные health-check-эндпоинты:

```bash
curl http://localhost:8080/api/health     # Go Backend
curl http://localhost:8081/health         # Whisper
curl http://localhost:11434/api/tags      # Ollama
```

## ⚙️ Настройка

| Переменная | По умолчанию | Описание |
|------------|-------------|----------|
| `GO_BACKEND_PORT` | 8080 | Порт веб-сервера |
| `GO_DB_PATH` | /data/meetings.db | Путь к базе данных |
| `WHISPER_MODEL` | base | Размер модели Whisper (tiny/base/small/large) |
| `WHISPER_CPU_MODE` | true | Работа на CPU (не использует GPU) |
| `OLLAMA_MODEL` | gemma4:12b | Имя модели в Ollama |
| `AUDIO_TIMEOUT_SECONDS` | 600 | Максимальное время транскрипции |
| `ANALYSIS_TIMEOUT_SECONDS` | 300 | Максимальное время генерации резюме |
| `MAX_UPLOAD_SIZE_MB` | 200 | Максимальный размер загружаемого файла |

## 💾 Данные

Все данные хранятся в Docker volumes:

```bash
docker volume ls | grep llama-local-test
```

- `llama-local-test_uploads` — оригинальные аудиофайлы
- `llama-local-test_exports` — экспортированные протоколы
- `llama-local-test_db-data` — SQLite база данных
- `llama-local-test_whisper-models` — модели Whisper
- `llama-local-test_ollama-models` — модели Ollama

Для полного удаления всех данных:

```bash
docker compose down -v
```

## ⚠️ Известные ограничения

1. **Скорость на CPU**: Transcription 1 часа аудио ≈ 15-30 минут.Gemmm4:12b ≈ 2-5 минут на стандартный протокол.
2. **Модель whisper**: `base` оптимизирована для скорости. Для точности измените `WHISPER_MODEL` на `small` или `medium` (больше RAM).
3. **Одна задача одновременно**: Go-бэкенд запускает обработку каждой встречи в отдельной горутине, но whisper-сервис обрабатывает запросы последовательно.
4. **Без диаризации**: Модель определяет текстовую транскрипцию без разделения по speakers (можно добавить в future releases).
5. **LLM может ошибаться**: Ответы модели недоверенные — интерфейс показывает валидацию JSON и понятные ошибки.

## 📁 Структура проекта

```
meetings/
├── .env.example              # Шаблоны переменных окружения
├── docker-compose.yml        # Оркестрация сервисов
├── README.md                 # Этот файл
│
├── backend-go/               # Go бэкенд
│   ├── Dockerfile            # Контейнер Go сервиса
│   ├── go.mod                # Зависимости Go
│   ├── main.go               # Точка входа
│   ├── models/               # Модели данных
│   │   ├── models.go         # Структуры Go
│   │   └── models_test.go    # Тесты моделей
│   ├── store/                # Хранилище (SQLite)
│   │   └── sqlite.go         # CRUD операции
│   ├── handlers/             # Обработчики HTTP
│   │   ├── handlers.go       # Upload, List, Get, Export
│   │   ├── routes.go         # Маршрутизация
│   │   └── handlers_test.go  # Тесты хендлеров
│   ├── client/               # Клиенты внешних сервисов
│   │   ├── whisper.go        # Whisper HTTP client
│   │   └── ollama.go         # Ollama HTTP client
│   └── frontend-static/      # Веб-интерфейс
│       ├── index.html        # Главная страница
│       ├── style.css         # Стили
│       └── app.js            # Клиентский JS
│
└── backend-python/           # Python Whisper-сервис
    ├── Dockerfile            # Контейнер Python сервиса
    ├── requirements.txt      # Зависимости Python
    └── app.py                # FastAPI приложение
```

## 🔄 Будущие улучшения

- [ ] Диаризация спикеров
- [ ] SRT-экспорт с таймкодами
- [ ] Поддержка нескольких языков
- [ ] Batch-обработка нескольких файлов
- [ ] Вебсокет-уведомления о завершении
- [ ] PDF-экспорт
- [ ] Telegram/Slack delivery (lokal'nyy relay)

## 📝 Лицензия

MIT. Вы можете использовать этот проект для личных или рабочих целей.

---

**Никаких облачных сервисов. Никаких API-ключей. Только ваша машина.**
