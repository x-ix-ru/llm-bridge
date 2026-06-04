# Spec 002 — Обновить docs/configuration.md

## Описание

Добавить секцию «Environment Variables / Переменные окружения» в `docs/configuration.md`.

## Полный контекст

Документ `docs/configuration.md` содержит полное описание YAML-конфигурации. Секция ENV должна быть добавлена как новая глава после «Configuration Persistence» и перед «Validation Rules».

## Технические детали реализации

### Новая секция в TOC

Добавить строку в TOC:
```
- [Environment Variables / Переменные окружения](#environment-variables--переменные-окружения)
```

### Новая секция

```markdown
## Environment Variables / Переменные окружения

All `GlobalConfig` fields can be overridden via environment variables.
ENV variables have the highest priority: **ENV > YAML > Default**.
Некорректные значения игнорируются (используется YAML или default).
Все ENV-переменные case-sensitive, верхний регистр.

**Примечание для `OPENCODE_BASE_URL`**: Если переменная установлена в пустую строку (`OPENCODE_BASE_URL=""`), это переопределяет значение из YAML на пустую строку. Это отличается от остальных полей, где пустой ENV игнорируется и используется YAML-значение.

| Variable | YAML Field | Type | Default | Description |
|---|---|---|---|---|
| `FALLBACK_STRATEGY` | `global.fallback_strategy` | string | `error` | Fallback strategy: `error`, `best_effort`, `queue` |
| `DISCOVERY_INTERVAL_SEC` | `global.discovery_interval_sec` | int | `15` | Poll interval for `/v1/models` |
| `REQUEST_TIMEOUT_SEC` | `global.request_timeout_sec` | int | `60` | Request timeout in seconds |
| `QUEUE_TIMEOUT_SEC` | `global.queue_timeout_sec` | int | `30` | Queue wait timeout in seconds |
| `DRAIN_TIMEOUT_SEC` | `global.drain_timeout_sec` | int | `30` | Graceful drain timeout |
| `SHUTDOWN_TIMEOUT_SEC` | `global.shutdown_timeout_sec` | int | `10` | Shutdown grace period |
| `OPENCODE_BASE_URL` | `global.opencode_base_url` | string | `""` | Base URL for opencode.jsonc generation |
| `OPENCODE_CONTEXT_BUFFER` | `global.opencode_context_buffer` | int | `4000` | Token buffer for OpenCode context window |
| `OPENCODE_CONTEXT_INPUT` | `global.opencode_context_input` | int | `0` | Input token allocation (0 = auto) |

**Existing variables** (not GlobalConfig overrides):

| Variable | Type | Default | Description |
|---|---|---|---|
| `CONFIG_PATH` | string | `config.yaml` | Path to YAML config file |
| `PORT` | string | `8080` | HTTP listen port |
```

## Файлы для изменения

- `docs/configuration.md`

## Критерии приёмки

- Секция «Environment Variables» присутствует в `docs/configuration.md`.
- TOC обновлён.
- Таблица содержит все 9 ENV-переменных + 2 существующих.
- Указан приоритет: ENV > YAML > Default.
- Документ читается без противоречий.
