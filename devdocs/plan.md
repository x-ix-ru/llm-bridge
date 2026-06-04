# План — ENV переменные для GlobalConfig

---

## Цель

Добавить поддержку ENV-переменных для всех полей `GlobalConfig`, которые переопределяют значения из YAML-конфига. Приоритет: ENV > YAML > Default.

---

## Список задач

| ID  | Название | Стэк | Статус |
|-----|----------|------|--------|
| 001 | Добавить helpers envInt/envString и логику ENV override в `config/config.go` | backend | new |
| 002 | Обновить `docs/configuration.md` — секция ENV переменных | docs | new |
| 003 | Обновить `README.md` — секция Environment Variables | docs | new |
| 004 | Добавить тесты ENV override в `config/config_test.go` | backend | new |

---

## Порядок выполнения и зависимости

```
001 ──► 002
    ──► 003
    ──► 004
```

- **001** — нет зависимостей, реализует ядро фичи.
- **002** — зависит от 001 (нужны точные имена ENV-переменных).
- **003** — зависит от 001 (нужны точные имена ENV-переменных).
- **004** — зависит от 001 (тестирует новую логику).

### Критический путь

001 → 004 (тесты подтверждают корректность реализации)

Задачи 002 и 003 (документация) могут быть выполнены параллельно с 004.

---

## Группы интеграционного тестирования

| Группа | Задачи | Описание |
|--------|--------|----------|
| A | 001 + 004 | Полный цикл: helpers → ENV override в Load() → unit-тесты подтверждения |

---

## ENV переменные

| ENV-переменная | Поле GlobalConfig | Тип | Default |
|---|---|---|---|
| `FALLBACK_STRATEGY` | `FallbackStrategy` | string | `"error"` |
| `DISCOVERY_INTERVAL_SEC` | `DiscoveryIntervalSec` | int | `15` |
| `REQUEST_TIMEOUT_SEC` | `RequestTimeoutSec` | int | `60` |
| `QUEUE_TIMEOUT_SEC` | `QueueTimeoutSec` | int | `30` |
| `DRAIN_TIMEOUT_SEC` | `DrainTimeoutSec` | int | `30` |
| `SHUTDOWN_TIMEOUT_SEC` | `ShutdownTimeoutSec` | int | `10` |
| `OPENCODE_BASE_URL` | `OpenCodeBaseURL` | string | `""` |
| `OPENCODE_CONTEXT_BUFFER` | `OpenCodeContextBuffer` | int | `4000` |
| `OPENCODE_CONTEXT_INPUT` | `OpenCodeContextInput` | int | `0` |

---

## Ожидаемый результат

После выполнения всех задач:
- Все поля `GlobalConfig` можно переопределить через ENV-переменные.
- ENV имеет приоритет над YAML: `ENV > YAML > Default`.
- Невалидные ENV-значения игнорируются (оставляется YAML/default значение).
- Документация `configuration.md` и `README.md` обновлены с секцией ENV переменных.
- Все тесты проходят: `go test ./config/...`
