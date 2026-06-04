# Spec 003 — Обновить README.md Environment Variables

## Описание

Обновить таблицу «Environment Variables» в `README.md`: добавить 9 строк для новых ENV-переменных `GlobalConfig`.

## Полный контекст

Секция «Environment Variables / Переменные окружения» в `README.md` сейчас содержит 2 строки (`CONFIG_PATH`, `PORT`). Нужно расширить таблицу до 11 строк.

## Технические детали реализации

Заменить текущую таблицу:

```markdown
| Variable | Description | Default |
|---|---|---|
| `CONFIG_PATH` | Path to YAML config file | `config.yaml` |
| `PORT` | HTTP listen port | `8080` |
```

На:

```markdown
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
```

Под таблицей добавить примечание:
```
All `GlobalConfig` fields can be overridden via environment variables. Priority: ENV > YAML > Default.
```

## Файлы для изменения

- `README.md`

## Критерии приёмки

- Таблица Environment Variables содержит 11 строк.
- Формат таблицы совпадает с существующим.
- Добавлено примечание о приоритете ENV > YAML > Default.
- README читается без противоречий.
