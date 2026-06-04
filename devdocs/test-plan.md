# План тестирования — ENV переменные для GlobalConfig

---

## Функциональное тестирование

### Task 001: ENV override helpers и логика

| # | Сценарий | Ожидаемый результат |
|---|----------|---------------------|
| 1 | `envInt("NONEXISTENT", 42)` | Возвращает 42 |
| 2 | `envInt("KEY", 42)` при `KEY=100` | Возвращает 100 |
| 3 | `envInt("KEY", 42)` при `KEY=abc` | Возвращает 42 (невалидный → default) |
| 4 | `envInt("KEY", 42)` при `KEY=-5` | Возвращает -5 (парсинг успешен) |
| 5 | `envString("NONEXISTENT", "def")` | Возвращает `"def"` |
| 6 | `envString("KEY", "def")` при `KEY=custom` | Возвращает `"custom"` |
| 7 | `Load()` без ENV | YAML-значение сохранено |
| 8 | `Load()` с ENV override | ENV-значение переопределило YAML |
| 9 | `Load()` с невалидным `FALLBACK_STRATEGY` | `FallbackError` (default) |
| 10 | `Load()` с отрицательным int ENV | Default (YAML-значение, прошедшее валидацию) |
| 11 | `Load()` с `OPENCODE_BASE_URL` ENV | Поле переопределено |
| 12 | `Load()` со всеми 9 ENV одновременно | Все поля переопределены корректно |

### Task 002: docs/configuration.md

| # | Сценарий | Ожидаемый результат |
|---|----------|---------------------|
| 1 | Секция ENV присутствует | Таблица с 9 переменными + 2 существующими |
| 2 | TOC обновлён | Ссылка на секцию ENV |
| 3 | Приоритет указан | ENV > YAML > Default |

### Task 003: README.md

| # | Сценарий | Ожидаемый результат |
|---|----------|---------------------|
| 1 | Таблица Environment Variables | 11 строк |
| 2 | Примечание о приоритете | Присутствует после таблицы |

---

## Интеграционное тестирование

### Группа A: ENV override → Load() → Tests (Tasks 001 + 004)

| # | Сценарий | Шаги | Ожидаемый результат |
|---|----------|------|---------------------|
| 1 | Full cycle: все ENV | 1. Установить все 9 ENV. 2. Создать Store. 3. `Load()`. 4. Проверить все поля. | Все поля GlobalConfig переопределены ENV. |
| 2 | Partial ENV | 1. Установить 3 ENV. 2. `Load()`. 3. Проверить. | 3 поля переопределены, остальные — YAML. |
| 3 | Mixed valid/invalid | 1. Установить 2 валидных + 1 невалидный ENV. 2. `Load()`. | 2 поля переопределены, невалидный → YAML. |
| 4 | No ENV at all | 1. Не устанавливать ENV. 2. `Load()` с кастомным YAML. | Все поля из YAML. |

---

## Регрессионное тестирование

| # | Функциональность | Тест | Ожидаемый результат |
|---|-----------------|------|---------------------|
| 1 | DefaultConfig | `TestDefaultConfig` | Без изменений |
| 2 | StoreLoadCreatesDefault | `TestStoreLoadCreatesDefault` | Без изменений |
| 3 | StoreLoadExisting | `TestStoreLoadExisting` | Без изменений (YAML-парсинг работает) |
| 4 | StoreSetInvalidStrategy | `TestStoreSetInvalidStrategy` | Без изменений |
| 5 | StoreSetInvalidServer | `TestStoreSetInvalidServer` | Без изменений |
| 6 | StoreSetValid | `TestStoreSetValid` | Без изменений |
| 7 | StoreSetPersists | `TestStoreSetPersists` | Без изменений |
| 8 | GetCopy | `TestStoreGetCopy` | Без изменений |
| 9 | FallbackStrategyValid | `TestFallbackStrategyValid` | Без изменений |
| 10 | OpenCode defaults | `TestOpenCodeContextDefaults` | Без изменений |
| 11 | OpenCode override | `TestOpenCodeContextLoad_Override` | Без изменений |
| 12 | OpenCode invalid | `TestOpenCodeContextSet_InvalidBuffer` | Без изменений |

---

## Автоматизация

Все тесты автоматизированы через `go test`:

- **Unit**: `go test ./config/... -v`
- **Full**: `go test ./...`

**Примечание**: Существующие ENV-переменные `CONFIG_PATH` и `PORT` читаются на уровне `cmd/llm-bridge/main.go` (вне пакета `config/`) и не затрагиваются этой фичей. Они остаются работоспособными без изменений.

Покрытие новых веток кода:
- `envInt()` — пустой ENV, валидный ENV, невалидный ENV, отрицательный ENV — `TestEnvInt_Helper`
- `envString()` — пустой ENV, установленный ENV — `TestEnvString_Helper`
- ENV override в `Load()` — полный, частичный, невалидный — `TestStoreLoad_ENVOverride*`
- Все 9 ENV одновременно — `TestStoreLoad_AllENVOverrides`
