# LLM Bridge / Мост LLM

> Proxy-сервер для маршрутизации запросов к кластеру LLM-серверов. Поддерживает OpenAI-совместимый API, автоматическое обнаружение моделей, Prometheus-метрики и встроенный админ-интерфейс.
> A proxy server for routing requests to an LLM server cluster. Supports OpenAI-compatible API, automatic model discovery, Prometheus metrics, and a built-in admin UI.

## Features / Возможности

- **Мульти-бэкенд** — подключение нескольких LLM-серверов с маршрутизацией на основе модели и distance
- **OpenAI-совместимый API** — /v1/chat/completions, /v1/completions, /v1/embeddings, /v1/models
- **Автоматическое обнаружение моделей** — периодический poll /v1/models каждого бэкенда
- **Гибкая маршрутизация** — distance + round-robin, fallback: error / best_effort / queue
- **Prometheus-метрики** — сбор метрик vLLM и internal metrics bridge
- **Админ-интерфейс** — SPA на чистом HTML/CSS/JS, встроен через embed.FS
- **Graceful shutdown** — SIGTERM/SIGINT, drain очереди, orderly stop
- **Single binary** — один статический бинарник, zero external dependencies

## Quick Start / Быстрый старт

### Docker Compose

```bash
docker compose up -d
```

Config file is mounted from `./config.yaml`. Default port: `8080`.

### Docker

```bash
# Build and run
docker build -t llm-bridge .
docker run -p 8080:8080 -v ./config.yaml:/config.yaml llm-bridge

# With custom port
docker run -p 9090:9090 -e PORT=9090 -v ./config.yaml:/config.yaml llm-bridge
```

### From Source / Из исходников

```bash
go build -o llm-bridge ./cmd/llm-bridge
./llm-bridge --config config.yaml --port 8080
```

Or with environment variables:
```bash
CONFIG_PATH=config.yaml PORT=8080 ./llm-bridge
```

## Configuration / Конфигурация

YAML config with global settings and server pool:

```yaml
global:
    fallback_strategy: best_effort   # error | best_effort | queue
    discovery_interval_sec: 15       # poll interval for /v1/models
    request_timeout_sec: 60          # request timeout
    queue_timeout_sec: 30            # queue wait timeout
    drain_timeout_sec: 30            # graceful drain timeout
    shutdown_timeout_sec: 10         # shutdown grace period

servers:
    - url: "http://localhost:8081"
      distance: 1                    # priority (1=highest, 10=lowest)
      max_concurrent_requests: 100   # max concurrent requests

    - url: "http://localhost:8082"
      distance: 2
      max_concurrent_requests: 50
```

### Config fields / Поля конфигурации

| Field | Type | Default | Description |
|---|---|---|---|
| `global.fallback_strategy` | string | `error` | Fallback when no server has the model |
| `global.discovery_interval_sec` | int | 15 | Interval to poll `/v1/models` |
| `global.request_timeout_sec` | int | 60 | Request timeout in seconds |
| `global.queue_timeout_sec` | int | 30 | Queue wait timeout in seconds |
| `global.drain_timeout_sec` | int | 30 | Graceful drain timeout |
| `global.shutdown_timeout_sec` | int | 10 | Shutdown grace period |
| `global.opencode_context_buffer` | int | `4000` | Total token budget for OpenCode (auto/explicit mode, see [Configuration docs](docs/configuration.md)) |
| `global.opencode_context_input` | int | `0` | Input token budget for OpenCode (0 = auto mode, >0 = explicit split) |
| `servers[].url` | string | — | Backend server URL (required) |
| `servers[].distance` | int | — | Priority (1=highest, 10=lowest) |
| `servers[].max_concurrent_requests` | int | — | Max concurrent requests |

## API Endpoints / Эндпоинты

### Proxy / Прокси

| Method | Endpoint | Description |
|---|---|---|
| `POST` | `/v1/chat/completions` | Chat completion |
| `POST` | `/v1/completions` | Text completion |
| `POST` | `/v1/embeddings` | Embeddings |
| `GET` | `/v1/models` | Aggregated model list |

### Admin REST API / Админ API

| Method | Endpoint | Description |
|---|---|---|
| `GET` | `/admin/servers` | List all servers |
| `POST` | `/admin/servers` | Add server |
| `GET` | `/admin/servers/{url}` | Get server |
| `PUT` | `/admin/servers/{url}` | Update server |
| `DELETE` | `/admin/servers/{url}` | Delete server |
| `GET` | `/admin/config` | View config |
| `PUT` | `/admin/config` | Update config (JSON or YAML) |
| `GET` | `/admin/status` | Cluster status |
| `GET` | `/admin/metrics` | Prometheus metrics |
| `GET` | `/admin/opencode-config` | Generated opencode.jsonc |

### Admin UI / Админ-интерфейс

| Page | URL | Description |
|---|---|---|
| Dashboard | `/admin/` | Overview and status |
| Servers | `/admin/servers` | Manage servers |
| Config | `/admin/config` | Edit configuration |
| Status | `/admin/status` | Cluster health |
| Chat | `/admin/chat` | Test chat with SSE streaming |
| Metrics | `/admin/metrics` | Prometheus metrics |

## Environment Variables / Переменные окружения

| Variable | Description | Default |
|---|---|---|
| `CONFIG_PATH` | Path to YAML config file | `config.yaml` |
| `PORT` | HTTP listen port | `8080` |
| `FALLBACK_STRATEGY` | Fallback strategy: `error`, `best_effort`, `queue` | `error` |
| `DISCOVERY_INTERVAL_SEC` | Poll interval for `/v1/models` | `15` |
| `REQUEST_TIMEOUT_SEC` | Request timeout in seconds | `60` |
| `QUEUE_TIMEOUT_SEC` | Queue wait timeout in seconds | `30` |
| `DRAIN_TIMEOUT_SEC` | Graceful drain timeout | `30` |
| `SHUTDOWN_TIMEOUT_SEC` | Shutdown grace period | `10` |
| `OPENCODE_BASE_URL` | Base URL for opencode.jsonc | `""` |
| `OPENCODE_CONTEXT_BUFFER` | Token buffer for OpenCode context | `4000` |
| `OPENCODE_CONTEXT_INPUT` | Input token allocation (0 = auto) | `0` |

All `GlobalConfig` fields can be overridden via environment variables. Priority: ENV > YAML > Default.

## License / Лицензия

MIT
