# План тестирования — Refactor OpenCode config generation

---

## Функциональное тестирование

### Task 001: Config fields

| # | Сценарий | Ожидаемый результат |
|---|----------|---------------------|
| 1 | `DefaultConfig()` | `OpenCodeContextBuffer = 4000`, `OpenCodeContextInput = 0` |
| 2 | YAML с `opencode_context_buffer: 2000` | Поле корректно парсится, значение = 2000 |
| 3 | YAML с `opencode_context_input: 8000` | Поле корректно парсится, значение = 8000 |
| 4 | YAML без новых полей (пустое) | Восстановлены defaults: 4000, 0 |
| 5 | `Set()` с отрицательным buffer | Ошибка валидации |
| 6 | `Set()` с отрицательным input | Ошибка валидации |
| 7 | `Load()` с buffer = -100 | Исправлено на 4000 |

### Task 002: Formula

| # | Сценарий | context | buffer | input_cfg | Expected input | Expected output |
|---|----------|---------|--------|-----------|----------------|-----------------|
| 1 | Auto mode (default) | 8192 | 4000 | 0 | 1192 | 3000 |
| 2 | Auto mode (large ctx) | 32768 | 4000 | 0 | 25768 | 3000 |
| 3 | Explicit input | 32768 | 4000 | 8000 | 8000 | 20768 |
| 4 | Custom buffer | 8192 | 2000 | 0 | 3192 | 3000 |
| 5 | Small context | 5000 | 4000 | 0 | 1000 | 3000 |
| 6 | Small context + explicit input | 5000 | 4000 | 3000 | 3000 | 3000 |
| 7 | MaxTokens branch | 8192 (max_tokens=5192 + 3000) | 4000 | 0 | 1192 | 3000 |

### Task 003: Tests

| # | Тест | Что проверяет |
|---|------|--------------|
| 1 | `TestOpenCodeConfig_Basic` | output=3000 при defaults |
| 2 | `TestOpenCodeConfig_WithMaxModelLen` | context=32768 → input=25768, output=3000 |
| 3 | `TestOpenCodeConfig_ContextLimitFromVLLM` | context=65536 → input=58536, output=3000 |
| 4 | `TestOpenCodeConfig_NoMaxModelLen` | context=8192 → input=1192, output=3000 |
| 5 | `TestOpenCodeConfig_CustomInput` | Явный input=8000 → output=20768 |
| 6 | `TestOpenCodeConfig_CustomBuffer` | buffer=2000 → input=3192, output=3000 |
| 7 | `TestOpenCodeConfig_SmallContext` | context=5000, auto → guards срабатывают |
| 8 | `TestOpenCodeConfig_SmallContextExplicit` | context=5000, explicit input=3000 → output guard срабатывает |
| 9 | `TestOpenCodeConfig_ConfigValidation` | Валидация новых полей |

---

## Интеграционное тестирование

### Группа A: Config → Formula → Output (Tasks 001 + 002 + 003)

| # | Сценарий | Шаги | Ожидаемый результат |
|---|----------|------|---------------------|
| 1 | Full cycle: default config | 1. Запустить сервер с default config. 2. GET /admin/opencode-config. 3. Проверить JSON. | output=3000 для всех моделей, input вычислен правильно по формуле. |
| 2 | Full cycle: custom config | 1. PUT /admin/config с custom buffer/input. 2. GET /admin/opencode-config. 3. Проверить JSON. | output вычислен по формуле `context - buffer - input`. |
| 3 | Config persistence | 1. Изменить config через API. 2. Проверить, что YAML файл сохранён. 3. Перезагрузить. 4. Проверить, что значения восстановлены. | Новые поля сохраняются и восстанавливаются. |
| 4 | Multiple models with different context | 1. Два бэкенда с разными max_model_len. 2. GET /admin/opencode-config. 3. Проверить каждую модель. | Каждая модель имеет корректные лимиты на свой context. |

---

## Регрессионное тестирование

| # | Функциональность | Тест | Ожидаемый результат |
|---|-----------------|------|---------------------|
| 1 | /v1/models | `TestGetModels` | Работает без изменений |
| 2 | /v1/chat/completions | `TestChatCompletions` | Работает без изменений |
| 3 | Admin CRUD servers | `TestAdmin*` | Работает без изменений |
| 4 | Admin config | `TestAdmin*Config*` | Работает, новые поля в JSON |
| 5 | Admin status | `TestAdminStatus` | Работает без изменений |
| 6 | Metrics | `TestMetricsEndpoint_*` | Работает без изменений |
| 7 | Smoke test | `TestIntegration_NoRegression` | Все endpoint работают |
| 8 | OpenCode base_url | `TestOpenCodeConfig_CustomBaseURL` | Работает без изменений |

---

## Автоматизация

Все тесты автоматизированы через `go test`:

- **Unit**: `go test ./config/...`
- **Unit + Integration**: `go test ./server/...`
- **Full**: `go test ./...`

Покрытие новых веток кода:
- Auto mode (input=0) — `TestOpenCodeConfig_Basic`
- Explicit mode (input>0) — `TestOpenCodeConfig_CustomInput`
- Custom buffer — `TestOpenCodeConfig_CustomBuffer`
- Guard (small context) — `TestOpenCodeConfig_SmallContext`, `TestOpenCodeConfig_SmallContextExplicit`
- Config validation — `TestOpenCodeConfig_ConfigValidation`
