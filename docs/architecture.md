# Architecture / Архитектура

---

## Содержание / Table of Contents

1. [Обзор / Overview](#overview)
2. [Компоненты / Components](#components)
3. [Поток данных / Data Flow](#data-flow)
4. [Модель конкурентности / Concurrency Model](#concurrency-model)
5. [Поток конфигурации / Configuration Flow](#configuration-flow)

---

## Обзор / Overview

LLM Bridge — это сервер-прокси, который маршрутизирует запросы к LLM API на кластер бэкенд-серверов. Реализован как единый Go-бинарь без внешних зависимостей во время выполнения.

LLM Bridge is a proxy server that routes LLM API requests to a cluster of backend servers. Built as a single Go binary with no external dependencies at runtime.

---

## Компоненты / Components

### 1. cmd/llm-bridge/main.go — Точка входа / Entry Point (~100 строк / ~100 lines)

Инициализирует все компоненты последовательно, настраивает обработку сигналов дляGraceful shutdown и orchestrates lifecycle:

Initializes all components in sequence, sets up signal handling for graceful shutdown, and orchestrates the lifecycle:

1. Загрузка конфигурации / Load config
2. Создание пула бэкендов / Create backend pool
3. Создание сервиса обнаружения / Create discovery service
4. Установка начальных серверов из конфигурации / Set initial servers from config
5. Создание роутера / Create router
6. Создание коллектора метрик / Create metrics collector
7. Создание HTTP-сервера / Create HTTP server
8. Запуск discovery (фоновая горута / background goroutine)
9. Запуск сбора метрик (фоновая горута / background goroutine)
10. Запуск HTTP-сервера (блокирующий / blocking)
11. По SIGTERM/SIGINT: остановка discovery → остановка метрик → draining очереди → shutdown HTTP

### 2. config/ — Слой конфигурации / Configuration Layer (~174 строки / ~174 lines)

- Конфигурация на YAML с персистентностью / YAML-based configuration persistence
- Потокобезопасный Store с RWMutex / Thread-safe Store with RWMutex
- Автоматическое создание файла конфигурации по умолчанию при первом запуске / Auto-creates default config file on first run
- Валидация конфигурации при сохранении (формат URL, distance 1-10, max_concurrent_requests > 0) / Validates config on save (URL format, distance 1-10, max_concurrent_requests > 0)

Типы данных / Data types:

```go
type GlobalConfig struct {
    FallbackStrategy     FallbackStrategy `yaml:"fallback_strategy"`     // error | best_effort | queue
    DiscoveryIntervalSec int              `yaml:"discovery_interval_sec"`
    RequestTimeoutSec    int              `yaml:"request_timeout_sec"`
    QueueTimeoutSec      int              `yaml:"queue_timeout_sec"`
    DrainTimeoutSec      int              `yaml:"drain_timeout_sec"`
    ShutdownTimeoutSec   int              `yaml:"shutdown_timeout_sec"`
    OpenCodeBaseURL      string           `yaml:"opencode_base_url,omitempty"`
}

type ServerConfig struct {
    URL                   string `yaml:"url"`
    Distance              int    `yaml:"distance"`
    MaxConcurrentRequests int    `yaml:"max_concurrent_requests"`
}
```

### 3. backend/ — Пул бэкендов / Backend Pool (~177 строк / ~177 lines)

- Управление HTTP-клиентами для каждого бэкенд-сервера / Manages HTTP clients for each backend server
- Отслеживание inflight-запросов через atomic-счётчики / Tracks inflight requests with atomic counters
- Connection pool: MaxIdleConns=100, MaxIdleConnsPerHost=10 / Connection pool with MaxIdleConns=100, MaxIdleConnsPerHost=10
- Streaming proxy: копирует заголовки и тело ответа напрямую / Streaming proxy: copies headers and response body directly

Ключевые операции / Key operations:

| Операция / Operation     | Описание / Description                                            |
|--------------------------|-------------------------------------------------------------------|
| `Acquire(serverID)`      | Возвращает Conn, увеличивает счётчик inflight / Returns Conn, increments inflight counter  |
| `Release(conn)`          | Уменьшает счётчик inflight / Decrements inflight counter          |
| `ProxyRequest(...)`      | Стриминг запроса на бэкенд / Streams request to backend           |

### 4. discovery/ — Обнаружение моделей / Model Discovery (~266 строк / ~266 lines)

- Периодический опрос GET /v1/models на каждом бэкенде / Periodic polling of GET /v1/models on each backend
- Отслеживание доступности моделей на серверах / Tracks which models are available on which servers
- Статус здоровья сервера (последний успешный опрос = healthy) / Health status per server (last successful poll = healthy)
- Сохранение полной метаданных моделей из upstream / Preserves full upstream model metadata

Структуры данных / Data structures:

```
models:       map[model_name][]server_url     — маппинг модель → серверы
modelDetails: map[model_name]json.RawMessage  — полные upstream метаданные
healthy:      map[server_url]bool             — статус здоровья
```

Интервал управляется через `global.discovery_interval_sec` (по умолчанию 15с). Таймаут HTTP-клиента: 10 секунд.

Interval controlled by `global.discovery_interval_sec` (default 15s). HTTP client timeout: 10 seconds.

### 5. router/ — Алгоритм маршрутизации / Routing Algorithm (~152 + 167 строк для queue / ~167 lines for queue)

Многоэтапный выбор сервера / Multi-stage server selection:

```
Stage 1: Отфильтровать серверы по доступности модели / Filter servers by model availability
Stage 2: Сортировка по distance (по возрастанию) / Sort by distance (ascending)
Stage 3: Группировка по одинаковому distance / Group by equal distance
Stage 4: Round-robin внутри каждой группы / Round-robin within each group
Stage 5: Пропуск серверов на максимальной пропускной способности / Skip servers at max capacity
Stage 6: Применение стратегии фоллбэка / Apply fallback strategy
```

Стратегии фоллбэка / Fallback strategies:

| Стратегия / Strategy | Описание / Description                                                   |
|----------------------|-------------------------------------------------------------------------|
| `error`              | Возвращает 503 Service Unavailable / Return 503 Service Unavailable     |
| `best_effort`        | Выбирает сервер с наименьшим соотношением inflight/maxConn / Selects server with lowest inflight/maxConn ratio |
| `queue`              | Ставит запрос в очередь с таймаутом, обрабатывает при появлении слота / Enqueue request with timeout, process when slot opens |

Ошибки / Errors:

| Ошибка / Error       | Описание / Description                                                  |
|----------------------|-------------------------------------------------------------------------|
| `ErrNoServers`       | Нет серверов с запрошенной моделью / No servers have the requested model |
| `ErrAllBusy`         | Все серверы на максимальной пропускной способности (без фоллбэка) / All servers at capacity (no fallback) |
| `ErrQueueTimeout`    | Ожидание в очереди превысило таймаут / Queue wait exceeded timeout      |

### 6. metrics/ — Prometheus Collector (~286 строк / ~286 lines)

- Периодический сбор `/metrics` с бэкендов vLLM / Periodic fetching of /metrics from vLLM backends
- Парсинг формата Prometheus от vLLM / Parses vLLM Prometheus format
- Вычисление скоростей (throughput) из кумулятивных счётчиков / Computes throughput rates from cumulative counters

Собираемые метрики / Collected metrics:

| Метрика / Metric                      | Описание / Description                   |
|---------------------------------------|------------------------------------------|
| `vllm:num_requests_running`           | Запросы в процессе исполнения            |
| `vllm:num_requests_waiting`           | Запросы в очереди                        |
| `vllm:kv_cache_usage_perc`            | Использование KV-кэша в процентах        |
| `vllm:prompt_tokens_total`            | Общее количество prompt-токенов          |
| `vllm:generation_tokens_total`        | Общее количество сгенерированных токенов |

Вычисление throughput / Throughput computation:

```
prefill_throughput = (current_prompt_tokens - prev_prompt_tokens) / dt
decode_throughput  = (current_gen_tokens - prev_gen_tokens) / dt
```

### 7. server/ — HTTP-сервер / HTTP Server (~591 строка / ~591 lines)

- Маршрутизатор chi.Mux / chi.Mux router
- Endpoints совместимые с OpenAI (4 маршрута) / OpenAI-compatible endpoints (4 routes)
- Admin REST API (10 маршрутов) / Admin REST API (10 routes)
- Admin SPA через embedded файлы / Admin SPA served via embedded files

Поток запроса / Request flow:

```
1. Клиент отправляет POST /v1/chat/completions
   Client sends POST /v1/chat/completions

2. Сервер читает тело, парсит JSON
   Server reads body, parses JSON

3. Router.Route(model) выбирает сервер
   Router.Route(model) selects server

4. Pool.Acquire(server) резервирует слот подключения
   Pool.Acquire(server) reserves connection slot

5. Pool.ProxyRequest стримит ответ обратно
   Pool.ProxyRequest streams response back

6. Pool.Release(conn) освобождает слот
   Pool.Release(conn) frees slot
```

Внутренние счётчики bridge (atomic.Int64) / Bridge internal counters:

| Счётчик / Counter    | Описание / Description                        |
|----------------------|-----------------------------------------------|
| `bridgeReqsSuccess`  | Общее количество успешных запросов            |
| `bridgeReqsError`    | Общее количество ошибочных запросов           |

### 8. web/ — Admin UI (~758 строк JS + 646 строк CSS)

- Одностраничное приложение на чистом JS / Single Page Application in vanilla JS
- Embedded через Go embed.FS / Embedded via Go embed.FS
- Тёмная тема, адаптивный дизайн / Dark theme, responsive design
- Страницы / Pages: Dashboard, Servers, Config, Status, Chat (SSE)

---

## Поток данных / Data Flow

```
Client
  │
  ▼
HTTP Server
  │
  ├──► Router ────────────► Backend Pool ──────────► Backend Server
  │        ▲                        ▲
  │        │                        │
  ▼        │                        │
Discovery  │                        │
(poll /v1) │                        │
  │        │                        │
  ▼        │                        │
models map │                        │
  │        │                        │
  └────────┘                        │
                                   │
Metrics                            │
(poll /metrics)                    │
  │                                │
  ▼                                │
metrics cache                      │
  │                                │
  ▼                                │
Admin UI ◄─────────────────────────┘
```

---

## Модель конкурентности / Concurrency Model

- Все обращения к состоянию защищены sync.RWMutex / All state access protected by sync.RWMutex
- Счётчики inflight используют atomic.Int64 / Inflight counts use atomic.Int64
- Фоновые горуты для discovery и сбора метрик / Background goroutines for discovery and metrics collection
- Graceful shutdown с отменой контекста / Graceful shutdown with context cancellation

---

## Поток конфигурации / Configuration Flow

```
config.yaml
    │
    ▼
Store.Load() ──────────► Config struct
    │
    ├──────────────────────────────────────┐
    ▼                                      ▼
HTTP PUT /admin/config              HTTP Server
    │
    ▼
Store.Set() ──────────► save to YAML file
    │
    ▼
syncServers() ─────────► pool + discovery + metrics updated
```
### 7. server/ — HTTP-сервер / HTTP Server (~700 строк / ~700 lines)

- Маршрутизатор chi.Mux / chi.Mux router
- Endpoints совместимые с OpenAI (4 маршрута) / OpenAI-compatible endpoints (4 routes)
- Admin REST API (10 маршрутов) / Admin REST API (10 routes)
- Admin SPA через embedded файлы / Admin SPA served via embedded files
- **Logging middleware** — логирует каждый запрос в JSON (slog) / **Logging middleware** — logs each request in JSON (slog)

### 4. discovery/ — Обнаружение моделей / Model Discovery (~300 строк / ~300 lines)

- Периодический опрос GET /v1/models на каждом бэкенде / Periodic polling of GET /v1/models on each backend
- Отслеживание доступности моделей на серверах / Tracks which models are available on which servers
- Статус здоровья сервера (последний успешный опрос = healthy) / Health status per server (last successful poll = healthy)
- Сохранение полной метаданных моделей из upstream / Preserves full upstream model metadata
- **Логирование статусов** — при изменении healthy/unhealthy логируется событие в stdout / **Status logging** — logs status change event to stdout on healthy/unhealthy transition

