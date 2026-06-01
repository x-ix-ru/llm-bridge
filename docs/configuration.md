# Configuration / Конфигурация

Detailed description of all configuration options for the llm-bridge proxy server.
Подробное описание всех параметров конфигурации прокси-сервера llm-bridge.

---

## Table of Contents / Содержание

- [Config File Structure / Структура файла конфигурации](#config-file-structure--структура-файла-конфигурации)
- [Global Settings / Глобальные настройки](#global-settings--глобальные-настройки)
  - [fallback_strategy / Стратегия резервирования](#fallback_strategy--стратегия-резервирования)
  - [discovery_interval_sec / Интервал обнаружения](#discovery_interval_sec--интервал-обнаружения)
  - [request_timeout_sec / Таймаут запроса](#request_timeout_sec--таймаут-запроса)
  - [queue_timeout_sec / Таймаут очереди](#queue_timeout_sec--таймаут-очереди)
  - [drain_timeout_sec / Таймаут дренажа](#drain_timeout_sec--таймаут-дренажа)
  - [shutdown_timeout_sec / Таймаут завершения](#shutdown_timeout_sec--таймаут-завершения)
  - [opencode_base_url / Базовый URL opencode](#opencode_base_url--базовый-url-opencode)
- [Server Configuration / Конфигурация серверов](#server-configuration--конфигурация-серверов)
  - [url / URL](#url--url)
  - [distance / Приоритет](#distance--приоритет)
  - [max_concurrent_requests / Максимальная параллельность](#max_concurrent_requests--максимальная-параллельность)
- [Configuration Loading / Загрузка конфигурации](#configuration-loading--загрузка-конфигурации)
- [Configuration Persistence / Сохранение конфигурации](#configuration-persistence--сохранение-конфигурации)
- [Validation Rules / Правила валидации](#validation-rules--правила-валидации)
- [Example Configurations / Примеры конфигураций](#example-configurations--примеры-конфигураций)

---

## Config File Structure / Структура файла конфигурации

The configuration file is written in YAML format.
Файл конфигурации написан в формате YAML.

```yaml
global:
    fallback_strategy: best_effort
    discovery_interval_sec: 15
    request_timeout_sec: 60
    queue_timeout_sec: 30
    drain_timeout_sec: 30
    shutdown_timeout_sec: 10
    opencode_base_url: "http://localhost:8080"

servers:
    - url: "http://localhost:8081"
      distance: 1
      max_concurrent_requests: 100
    - url: "http://localhost:8082"
      distance: 2
      max_concurrent_requests: 50
```

---

## Global Settings / Глобальные настройки

### fallback_strategy / Стратегия резервирования

Type / Тип: `string`
Default / По умолчанию: `"error"`

Defines how the router handles requests when no server can immediately accept them.
Определяет, как маршрутизатор обрабатывает запросы, когда ни один сервер не может их немедленно принять.

Options / Варианты:

| Option / Вариант  | Description / Описание |
|-------------------|------------------------|
| `error`           | Return `503 Service Unavailable` immediately if no server can handle the request. Возвращает `503 Service Unavailable` немедленно, если ни один сервер не может обработать запрос. |
| `best_effort`     | Try the server with the lowest load (`inflight/maxConn` ratio). Попробует сервер с наименьшей загрузкой (отношение `inflight/maxConn`). |
| `queue`           | Queue the request and wait for a slot to open. Поставит запрос в очередь и будет ждать освобождения слота. |

---

### discovery_interval_sec / Интервал обнаружения

Type / Тип: `int`
Default / По умолчанию: `15`
Range / Диапазон: `> 0` seconds/секунд

How often to poll `GET /v1/models` on each backend server. Controls how quickly model changes (added/removed models) are detected and reflected in the proxy.
Как часто опрашивать `GET /v1/models` на каждом бэкенд-сервере. Определяет, насколько быстро обнаруживаются изменения моделей (добавление/удаление моделей) и отражаются в прокси.

---

### request_timeout_sec / Таймаут запроса

Type / Тип: `int`
Default / По умолчанию: `60`
Range / Диапазон: `> 0` seconds/секунд

Timeout for HTTP requests proxied to backends. This is the total HTTP request timeout, including connection time, headers, and response body.
Таймаут для HTTP-запросов, проксируемых на бэкенды. Это общий таймаут HTTP-запроса, включающий время подключения, заголовки и тело ответа.

---

### queue_timeout_sec / Таймаут очереди

Type / Тип: `int`
Default / По умолчанию: `30`
Range / Диапазон: `> 0` seconds/секунд

When `fallback_strategy` is `"queue"`, the maximum time a request will wait in the queue before returning `503`.
Когда `fallback_strategy` установлен в `"queue"`, максимальное время ожидания запроса в очереди перед возвратом `503`.

---

### drain_timeout_sec / Таймаут дренажа

Type / Тип: `int`
Default / По умолчанию: `30`
Range / Диапазон: `> 0` seconds/секунд

Timeout for draining the request queue during graceful shutdown. Active requests continue processing until this timeout expires.
Таймаут для завершения обработки запросов в очереди во время корректного завершения работы. Активные запросы продолжают обрабатываться до истечения этого таймаута.

---

### shutdown_timeout_sec / Таймаут завершения

Type / Тип: `int`
Default / По умолчанию: `10`
Range / Диапазон: `> 0` seconds/секунд

Grace period for the HTTP server to close listeners and accept draining requests. After this timeout, the server forcefully terminates.
Время на корректное завершение работы HTTP-сервера: закрытие слушателей и завершение обрабатываемых запросов. По истечении этого таймаута сервер принудительно завершается.

---

### opencode_base_url / Базовый URL opencode

Type / Тип: `string` (optional/опционально)

Base URL used when generating `opencode.jsonc` configuration. Defaults to the bridge address if not set.
Базовый URL, используемый при генерации конфигурации `opencode.jsonc`. Если не установлен, по умолчанию используется адрес прокси.

---

## Server Configuration / Конфигурация серверов

### url / URL

Type / Тип: `string` (required/обязательно)

Backend server URL. Must be a valid URL (checked on save).
URL бэкенд-сервера. Должен быть корректным URL (проверяется при сохранении).

Examples / Примеры:

```yaml
# Local vLLM instance / Локальный экземпляр vLLM
url: "http://localhost:8081"

# Remote OpenAI-compatible API / Удалённый API, совместимый с OpenAI
url: "https://api.openai.com/v1"
```

---

### distance / Приоритет

Type / Тип: `int`
Range / Диапазон: `1–10` (validated on save/валидируется при сохранении)

Priority level. Lower values mean higher priority. Used for server selection ordering.
Уровень приоритета. Меньшие значения означают более высокий приоритет. Используется для порядка выбора серверов.

- `1` = highest priority / самый высокий приоритет
- `10` = lowest priority / самый низкий приоритет

The router always prefers servers with a lower distance value. Only when all servers at a given distance level are at capacity does it proceed to the next level.
Маршрутизатор всегда отдаёт предпочтение серверам с меньшим значением distance. Только когда все серверы на текущем уровне загрузки, он переходит к следующему уровню.

Example scenario / Пример:

```yaml
servers:
    - url: "http://localhost:8081"  # distance: 1 - primary, cheapest / основной, самый дешёвый
      distance: 1
    - url: "http://localhost:8082"  # distance: 2 - secondary / дополнительный
      distance: 2
    - url: "http://localhost:8083"  # distance: 5 - fallback only / только резервный
      distance: 5
```

---

### max_concurrent_requests / Максимальная параллельность

Type / Тип: `int`
Range / Диапазон: `> 0` (validated on save/валидируется при сохранении)

Maximum number of simultaneous requests to this server. Servers exceeding this limit are skipped by the router.
Максимальное количество одновременных запросов к серверу. Серверы, превышающие этот лимит, пропускаются маршрутизатором.

This acts as a circuit breaker to prevent overloading individual backends.
Выступает в роли предохранителя (circuit breaker), предотвращающего перегрузку отдельных бэкендов.

---

## Configuration Loading / Загрузка конфигурации

The config is loaded from a YAML file. Path resolution order:
Конфигурация загружается из YAML-файла. Порядок определения пути:

1. Command-line flag / Флаг командной строки: `--config config.yaml`
2. Environment variable / Переменная окружения: `CONFIG_PATH=config.yaml`
3. Default / По умолчанию: `config.yaml` in the current directory/текущей директории

---

## Configuration Persistence / Сохранение конфигурации

- On first run with no config file: auto-creates `config.yaml` with defaults.
  При первом запуске без файла конфигурации: автоматически создаёт `config.yaml` со значениями по умолчанию.
- Changes via admin API (`PUT /admin/config`): persists to YAML file immediately.
  Изменения через админ-API (`PUT /admin/config`): сохраняются в YAML-файл немедленно.
- Thread-safe: all config reads/writes protected by RWMutex.
  Потокобезопасно: все операции чтения/записи защищены RWMutex.

---

## Validation Rules / Правила валидации

When updating config (via API or file), the following is validated:
При обновлении конфигурации (через API или файл) проверяются следующие правила:

- Each server URL must be a valid URL (via `url.Parse`).
  Каждый URL сервера должен быть корректным (через `url.Parse`).
- Each server distance must be between 1 and 10.
  Приоритет каждого сервера должен быть в диапазоне от 1 до 10.
- Each server `max_concurrent_requests` must be greater than 0.
  Максимальная параллельность каждого сервера должна быть больше 0.
- `fallback_strategy` must be one of: `error`, `best_effort`, `queue`.
  `fallback_strategy` должен быть одним из: `error`, `best_effort`, `queue`.

Invalid config is rejected with a `400 Bad Request` error.
Некорректная конфигурация отклоняется с ошибкой `400 Bad Request`.

---

## Example Configurations / Примеры конфигураций

### Minimal (single server) / Минимальная (один сервер)

```yaml
global:
    fallback_strategy: error
    discovery_interval_sec: 15
    request_timeout_sec: 60
    queue_timeout_sec: 30
    drain_timeout_sec: 30
    shutdown_timeout_sec: 10
servers:
    - url: "http://localhost:8081"
      distance: 1
      max_concurrent_requests: 100
```

### Multi-provider with fallback / Мультипоставщик с резервированием

```yaml
global:
    fallback_strategy: best_effort
    discovery_interval_sec: 10
    request_timeout_sec: 120
    queue_timeout_sec: 60
    drain_timeout_sec: 30
    shutdown_timeout_sec: 10
servers:
    - url: "http://vllm-1:8000"
      distance: 1
      max_concurrent_requests: 200
    - url: "http://vllm-2:8000"
      distance: 1
      max_concurrent_requests: 200
    - url: "https://api.openai.com/v1"
      distance: 3
      max_concurrent_requests: 50
```

### Production with queuing / Производство с очередью

```yaml
global:
    fallback_strategy: queue
    discovery_interval_sec: 5
    request_timeout_sec: 300
    queue_timeout_sec: 120
    drain_timeout_sec: 60
    shutdown_timeout_sec: 15
servers:
    - url: "http://vllm-prod-1:8000"
      distance: 1
      max_concurrent_requests: 500
    - url: "http://vllm-prod-2:8000"
      distance: 1
      max_concurrent_requests: 500
    - url: "http://vllm-prod-3:8000"
      distance: 1
      max_concurrent_requests: 500
```
