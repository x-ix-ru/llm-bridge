# API Documentation / Документация API

This documentation describes all endpoints of the LLM Bridge proxy server.
В этой документации описаны все эндпоинты прокси-сервера LLM Bridge.

---

## Table of Contents / Содержание

- [Common Behavior / Общее поведение](#common-behavior)
- [Proxy Endpoints / Прокси-эндпоинты](#proxy-endpoints)
  - [POST /v1/chat/completions](#post-v1chatcompletions)
  - [POST /v1/completions](#post-v1completions)
  - [POST /v1/embeddings](#post-v1embeddings)
  - [GET /v1/models](#get-v1models)
- [Admin Endpoints / Административные эндпоинты](#admin-endpoints)
  - [GET /admin/servers](#get-adminservers)
  - [POST /admin/servers](#post-adminservers)
  - [GET /admin/servers/{url}](#get-adminserversurl)
  - [PUT /admin/servers/{url}](#put-adminserversurl)
  - [DELETE /admin/servers/{url}](#delete-adminserversurl)
  - [GET /admin/config](#get-adminconfig)
  - [PUT /admin/config](#put-adminconfig)
  - [GET /admin/status](#get-adminstatus)
  - [GET /admin/metrics](#get-adminmetrics)
  - [GET /admin/opencode-config](#get-adminopencode-config)
- [Admin UI / Административный интерфейс](#admin-ui)
- [Routing Algorithm / Алгоритм маршрутизации](#routing-algorithm)
- [Error Handling / Обработка ошибок](#error-handling)

---

## Common Behavior / Общее поведение

All proxy endpoints share a common implementation in `server.go`'s `handleOpenAIProxy`.
Все прокси-эндпоинты имеют общую реализацию в `handleOpenAIProxy` файла `server.go`.

The proxy processing pipeline:
Процесс обработки прокси:

1. Read request body as bytes.
   Считать тело запроса в байты.

2. Parse JSON to extract the `"model"` field (required) and `"stream"` boolean.
   Распарсить JSON для извлечения поля `"model"` (обязательное) и булева значения `"stream"`.

3. Call `router.Route(model)` to select a backend server.
   Вызвать `router.Route(model)` для выбора сервера-бэкенда.

4. Proxy the request to the selected backend (copy headers, stream response body).
   Проксировать запрос на выбранный бэкенд (копировать заголовки, стримить тело ответа).

5. For streaming requests, set `Content-Type: text/event-stream`.
   Для стриминговых запросов установить `Content-Type: text/event-stream`.

---

## Proxy Endpoints / Прокси-эндпоинты

All proxy endpoints are compatible with the OpenAI API format.
Все прокси-эндпоинты совместимы с форматом API OpenAI.

### POST /v1/chat/completions

**RU** — Стандартный эндпоинт чат-завершений OpenAI. Поддерживает стриминг (SSE).

**EN** — Standard OpenAI chat completions endpoint. Supports streaming (SSE).

**Request / Запрос:**

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `model` | string | yes | Model name to route the request |
| `messages` | array | yes | Array of chat messages |
| `stream` | boolean | no | Enable streaming response (default: `false`) |
| `temperature` | number | no | Sampling temperature |
| `max_tokens` | integer | no | Maximum number of tokens to generate |

**Non-Streaming / Без стриминга:**

```bash
curl -X POST http://localhost:8080/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{
    "model": "gpt-4o",
    "messages": [{"role": "user", "content": "Hello"}],
    "stream": false
  }'
```

**Streaming / Стриминг:**

```bash
curl -X POST http://localhost:8080/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{
    "model": "gpt-4o",
    "messages": [{"role": "user", "content": "Hello"}],
    "stream": true
  }'
```

---

### POST /v1/completions

**RU** — Стандартный эндпоинт текстовых завершений OpenAI. Стриминг не поддерживается.

**EN** — Standard OpenAI text completions endpoint. Does not support streaming.

**Request / Запрос:**

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `model` | string | yes | Model name to route the request |
| `prompt` | string | yes | The text to complete |
| `max_tokens` | integer | no | Maximum number of tokens to generate |

```bash
curl -X POST http://localhost:8080/v1/completions \
  -H "Content-Type: application/json" \
  -d '{
    "model": "gpt-4o",
    "prompt": "Once upon a time"
  }'
```

---

### POST /v1/embeddings

**RU** — Стандартный эндпоинт эмбеддингов OpenAI.

**EN** — Standard OpenAI embeddings endpoint.

**Request / Запрос:**

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `model` | string | yes | Model name to generate embeddings |
| `input` | string/array | yes | Input text or array of texts |

```bash
curl -X POST http://localhost:8080/v1/embeddings \
  -H "Content-Type: application/json" \
  -d '{
    "model": "text-embedding-3-small",
    "input": "Hello world"
  }'
```

---

### GET /v1/models

**RU** — Возвращает объединённый список моделей со всех бэкендов. Полная метаданные от первого сервера, объявившего модель, сохраняются.

**EN** — Returns an aggregated list of models from all backends. Preserves full upstream metadata from the first server that advertises each model.

**Response / Ответ:**

```json
{
  "object": "list",
  "data": [
    {"id": "gpt-4o", "object": "model"},
    {"id": "claude-sonnet", "object": "model"},
    {"id": "text-embedding-3-small", "object": "model"}
  ]
}
```

```bash
curl http://localhost:8080/v1/models
```

---

## Admin Endpoints / Административные эндпоинты

### GET /admin/servers

**RU** — Список всех серверов с текущим состоянием.

**EN** — List all servers with runtime state.

**Response / Ответ:**

```json
[
  {
    "url": "http://localhost:8081",
    "distance": 1,
    "max_concurrent_requests": 100,
    "healthy": true,
    "inflight": 5
  },
  {
    "url": "http://localhost:8082",
    "distance": 2,
    "max_concurrent_requests": 50,
    "healthy": false,
    "inflight": 0
  }
]
```

```bash
curl http://localhost:8080/admin/servers
```

---

### POST /admin/servers

**RU** — Добавить новый сервер в кластер.

**EN** — Add a new server to the cluster.

**Request body / Тело запроса:**

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `url` | string | yes | Backend server URL |
| `distance` | integer | no | Routing priority (lower = higher priority, default: 1) |
| `max_concurrent_requests` | integer | no | Maximum concurrent requests (default: 100) |

**Request / Запрос:**

```bash
curl -X POST http://localhost:8080/admin/servers \
  -H "Content-Type: application/json" \
  -d '{
    "url": "http://localhost:8083",
    "distance": 3,
    "max_concurrent_requests": 50
  }'
```

**Response / Ответ:**

- `201 Created` — Server added, returns the server config.
- `409 Conflict` — Server URL already exists.

---

### GET /admin/servers/{url}

**RU** — Получить один сервер по его URL (URL-encoded).

**EN** — Get a single server by its URL (URL-encoded).

```bash
curl http://localhost:8080/admin/servers/http%3A%2F%2Flocalhost%3A8081
```

---

### PUT /admin/servers/{url}

**RU** — Обновить конфигурацию существующего сервера. Поддерживаются частичные обновления (изменяются только указанные поля).

**EN** — Update an existing server's configuration. Partial updates supported (only provided fields are changed).

**Request body / Тело запроса:**

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `distance` | integer | no | New routing priority |
| `max_concurrent_requests` | integer | no | New max concurrent requests |

```bash
curl -X PUT http://localhost:8080/admin/servers/http%3A%2F%2Flocalhost%3A8081 \
  -H "Content-Type: application/json" \
  -d '{
    "distance": 2
  }'
```

---

### DELETE /admin/servers/{url}

**RU** — Удалить сервер из кластера.

**EN** — Remove a server from the cluster.

**Response / Ответ:**

- `204 No Content` — Server removed successfully.

```bash
curl -X DELETE http://localhost:8080/admin/servers/http%3A%2F%2Flocalhost%3A8081
```

---

### GET /admin/config

**RU** — Вернуть текущую конфигурацию в формате JSON.

**EN** — Return current configuration as JSON.

**Response / Ответ:**

```json
{
  "global": {
    "fallback_strategy": "best_effort",
    "discovery_interval_sec": 15,
    "request_timeout_sec": 60,
    "queue_timeout_sec": 30,
    "drain_timeout_sec": 30,
    "shutdown_timeout_sec": 10
  },
  "servers": [
    {"url": "http://localhost:8081", "distance": 1, "max_concurrent_requests": 100},
    {"url": "http://localhost:8082", "distance": 2, "max_concurrent_requests": 50}
  ]
}
```

```bash
curl http://localhost:8080/admin/config
```

---

### PUT /admin/config

**RU** — Обновить всю конфигурацию. Принимает JSON, YAML или автоопределение формата. Заголовок `Content-Type` устанавливается в `application/json` или `application/yaml` в зависимости от формата.

**EN** — Update the entire configuration. Accepts JSON, YAML, or auto-detects. Sets `Content-Type` header to `application/json` or `application/yaml` to indicate format.

```bash
curl -X PUT http://localhost:8080/admin/config \
  -H "Content-Type: application/json" \
  -d '{
    "global": {
      "fallback_strategy": "queue",
      "discovery_interval_sec": 30,
      "request_timeout_sec": 120
    },
    "servers": [
      {"url": "http://localhost:8081", "distance": 1, "max_concurrent_requests": 200}
    ]
  }'
```

---

### GET /admin/status

**RU** — Подробный статус кластера, включая модели, здоровье серверов и метрики vLLM.

**EN** — Detailed cluster status including models, health, and vLLM metrics.

**Response / Ответ:**

```json
{
  "servers": [
    {
      "url": "http://localhost:8081",
      "distance": 1,
      "max_concurrent_requests": 100,
      "healthy": true,
      "inflight": 5,
      "metrics": {
        "requests_running": 3,
        "requests_waiting": 0,
        "kv_cache_usage_perc": 45.2,
        "prompt_tokens_total": 150000,
        "gen_tokens_total": 320000,
        "prefill_throughput": 1250.5,
        "decode_throughput": 8500.3,
        "avg_prefill_time_ms": 2.4,
        "avg_decode_time_ms": 0.8,
        "updated_at": "2025-01-01T00:00:00Z"
      }
    }
  ],
  "models": {
    "gpt-4o": ["http://localhost:8081", "http://localhost:8082"],
    "claude-sonnet": ["http://localhost:8082"]
  },
  "healthy": {
    "http://localhost:8081": true,
    "http://localhost:8082": true
  }
}
```

```bash
curl http://localhost:8080/admin/status
```

**vLLM Metrics / Метрики vLLM:**

| Field | Type | Description / Описание |
|-------|------|------------------------|
| `requests_running` | integer | Текущие выполняющиеся запросы в vLLM / Current running requests on vLLM |
| `requests_waiting` | integer | Запросы в очереди vLLM / Queued requests in vLLM |
| `kv_cache_usage_perc` | float | Использование KV-кэша в процентах (0–100) / KV cache usage percentage (0–100) |
| `prompt_tokens_total` | integer | Кумулятивные токены запросов / Cumulative prompt tokens |
| `gen_tokens_total` | integer | Кумулятивные сгенерированные токены / Cumulative generated tokens |
| `prefill_throughput` | float | Скорость префилла токенов/с (вычисленная) / Tokens/s prefill rate (computed) |
| `decode_throughput` | float | Скорость декодирования токенов/с (вычисленная) / Tokens/s decode rate (computed) |
| `avg_prefill_time_ms` | float | Среднее время префилла на токен / Average prefill time per token |
| `avg_decode_time_ms` | float | Среднее время декодирования на токен / Average decode time per token |
| `updated_at` | string | Время последнего обновления метрик / Timestamp of last metrics update |

---

### GET /admin/metrics

**RU** — Экспортировать Prometheus-метрики в текстовом формате exposition.

**EN** — Expose Prometheus metrics in text exposition format (version=0.0.4).

**Response:**

- **Content-Type / Тип контента:** `text/plain; version=0.0.4; charset=utf-8`

**Included metrics / Включённые метрики:**

| Metric | Type | Description / Описание |
|--------|------|------------------------|
| `vllm_<field>` | gauge/counter | Метрики vLLM со всех серверов, префикс `vllm_` / vLLM metrics from all servers, prefixed with `vllm_` |
| `bridge_requests_total_success` | counter | Успешные запросы через прокси / Successful requests through the proxy |
| `bridge_requests_total_error` | counter | Ошибочные запросы через прокси / Failed requests through the proxy |
| `bridge_server_inflight` | gauge | Текущие активные запросы на каждом сервере / Current inflight requests per server |

```bash
curl http://localhost:8080/admin/metrics
```

---

### GET /admin/opencode-config

**RU** — Сгенерировать конфигурационный файл для OpenCode (opencode.jsonc).

**EN** — Generate OpenCode configuration file content (opencode.jsonc).

```bash
curl http://localhost:8080/admin/opencode-config
```

---

## Admin UI / Административный интерфейс

**RU** — Встроенный веб-интерфейс для управления кластером.

**EN** — Built-in web interface for cluster management.

| Page / Страница | URL | Description / Описание |
|-----------------|-----|------------------------|
| Dashboard / Панель | `/admin/` | Общая информация и статус кластера / Overview and status |
| Servers / Серверы | `/admin/servers` | Управление серверами кластера / Manage servers |
| Config / Конфигурация | `/admin/config` | Редактирование конфигурации / Edit configuration |
| Status / Статус | `/admin/status` | Состояние и здоровье кластера / Cluster health |
| Chat / Чат | `/admin/chat` | Тестирование чата со стримингом SSE / Test chat with SSE streaming |
| Metrics / Метрики | `/admin/metrics` — Prometheus-метрики кластера / Prometheus metrics |

---

## Routing Algorithm / Алгоритм маршрутизации

**RU** — Роутер использует многоэтапный алгоритм для выбора сервера:

**EN** — The router uses a multi-stage algorithm:

1. **Filtering / Фильтрация** — Select servers that have the requested model.
   Выбрать серверы, на которых доступна запрашиваемая модель.

2. **Sorting / Сортировка** — Sort by `distance` (lower value = higher priority).
   Сортировать по `distance` (меньше = выше приоритет).

3. **Grouping / Группировка** — Group servers with the same distance.
   Сгруппировать серверы с одинаковым `distance`.

4. **Round-Robin / Круговой обмен** — Within each group, use round-robin selection.
   В пределах каждой группы использовать round-robin выбор.

5. **Skip full capacity / Пропуск заполненных** — Servers where `inflight >= max_concurrent_requests` are skipped.
   Серверы, где `inflight >= max_concurrent_requests`, пропускаются.

6. **Fallback strategy / Стратегия резервирования**:

   | Strategy / Стратегия | Behavior / Поведение |
   |----------------------|----------------------|
   | `error` | Вернуть 503, если сервер недоступен / Return 503 if no server available |
   | `best_effort` | Выбрать сервер с наименьшим соотношением inflight/maxConn / Select server with lowest inflight/maxConn ratio |
   | `queue` | Поставить запрос в очередь с настраиваемым таймаутом / Put request in a queue with configurable timeout |

---

## Error Handling / Обработка ошибок

All error responses follow the OpenAI error format.
Все ответы об ошибках следуют формату ошибок OpenAI.

```json
{"error": {"message": "description", "type": "error"}}
```

**HTTP Status Codes / Коды состояния HTTP:**

| Code | Condition / Условие | RU Description / Описание |
|------|---------------------|---------------------------|
| `400` | Invalid JSON, missing `model`, body read error | Неверный JSON, отсутствует `model`, ошибка чтения тела |
| `422` | Model not found on any server | Модель не найдена ни на одном сервере |
| `502` | Proxy error to backend | Ошибка проксирования на бэкенд |
| `503` | No servers available, all servers at capacity, queue timeout, or request canceled | Нет доступных серверов, все серверы загружены, таймаут очереди или запрос отменён |
