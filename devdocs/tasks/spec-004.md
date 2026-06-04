# Spec 004 — Тесты ENV override

## Описание

Добавить unit-тесты для ENV-override логики в `config/config_test.go`.

## Полный контекст

Тесты должны покрывать:
- Helper-функции `envInt()` и `envString()`.
- `Store.Load()` с ENV-переопределениями.
- Приоритет: ENV > YAML > Default.
- Невалидные ENV-значения игнорируются.

## Технические детали реализации

Все тесты используют `t.Setenv()` для установки ENV и `t.TempDir()` для временных YAML-файлов.

### Тестовые функции

#### `TestEnvInt_Helper`
| # | ENV | defaultVal | Expected |
|---|---|---|---|
| 1 | Не установлена | 42 | 42 |
| 2 | `""` (пустой) | 42 | 42 |
| 3 | `"100"` | 42 | 100 |
| 4 | `"abc"` (не число) | 42 | 42 |
| 5 | `"-5"` | 42 | -5 |

#### `TestEnvString_Helper`
| # | ENV | defaultVal | Expected |
|---|---|---|---|
| 1 | Не установлена | `"default"` | `"default"` |
| 2 | `""` (пустой) | `"default"` | `"default"` |
| 3 | `"custom"` | `"default"` | `"custom"` |

#### `TestStoreLoad_ENVOverrideIntFields`
YAML: `discovery_interval_sec: 15`. ENV: `DISCOVERY_INTERVAL_SEC=99`. Ожидаемый результат: 99.

#### `TestStoreLoad_ENVOverrideStringField`
YAML: `fallback_strategy: best_effort`. ENV: `FALLBACK_STRATEGY=queue`. Ожидаемый результат: `queue`.

#### `TestStoreLoad_ENVOverrideInvalidInt`
YAML: `discovery_interval_sec: 15`. ENV: `DISCOVERY_INTERVAL_SEC=abc`. Ожидаемый результат: 15 (YAML сохранён).

#### `TestStoreLoad_ENVOverrideInvalidFallback`
YAML: `fallback_strategy: best_effort`. ENV: `FALLBACK_STRATEGY=invalid`. Ожидаемый результат: `error` (default для невалидной стратегии).

#### `TestStoreLoad_ENVOverrideNegativeInt`
YAML: `request_timeout_sec: 60`. ENV: `REQUEST_TIMEOUT_SEC=-10`. Ожидаемый результат: 60 (default, т.к. <=0).

#### `TestStoreLoad_ENVNotSet_UsesYAML`
YAML: `request_timeout_sec: 120`. ENV: не установлена. Ожидаемый результат: 120.

#### `TestStoreLoad_ENVOverrideOpenCodeFields`
YAML: `opencode_context_buffer: 4000, opencode_context_input: 0`. ENV: `OPENCODE_CONTEXT_BUFFER=8000, OPENCODE_CONTEXT_INPUT=5000`. Ожидаемый результат: 8000, 5000.

#### `TestStoreLoad_ENVOverrideOpenCodeBaseURL`
YAML: без `opencode_base_url`. ENV: `OPENCODE_BASE_URL=http://mybridge:9090`. Ожидаемый результат: `http://mybridge:9090`.

#### `TestStoreLoad_AllENVOverrides`
Интеграционный тест: все 9 ENV-переменных установлены одновременно. Проверить каждое поле.

## Файлы для изменения

- `config/config_test.go` — добавление новых тестовых функций.

## Критерии приёмки

- Все тестовые функции из таблицы выше реализованы.
- `go test ./config/...` проходит без ошибок.
- Тесты используют `t.Setenv()` (изолированные, не влияют на другие тесты).
- Тесты используют `t.TempDir()` для временных YAML-файлов.
