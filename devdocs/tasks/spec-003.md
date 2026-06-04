# Спецификация Task 003 — Тесты OpenCode Config с новой формулой

## Описание

Обновить существующие тесты и добавить новые тест-кейсы для проверки корректности новой формулы расчёта лимитов в `handleOpenCodeConfig()`.

## Полный контекст

### Существующие тесты (server/server_test.go):

| Тест | Что проверяет | Что изменится |
|------|--------------|---------------|
| `TestOpenCodeConfig_Basic` | Базовая генерация, `"output": 3000` | output stays 3000 (auto mode). Input changes: was `ctx-4000`, now `ctx-4000-3000`. |
| `TestOpenCodeConfig_WithMaxModelLen` | context=32768, input=28768, output=3000 | input changes from 28768 to 25768 (32768-4000-3000). Output stays 3000. |
| `TestOpenCodeConfig_CustomBaseURL` | Custom base_url query param | No change (не зависит от лимитов). |
| `TestOpenCodeConfig_NoModels` | Пустой список моделей | No change. |
| `TestOpenCodeConfig_ContextLimitFromVLLM` | context=65536, input=61536, output=3000 | input changes from 61536 to 58536 (65536-4000-3000). Output stays 3000. |
| `TestOpenCodeConfig_NoMaxModelLen` | context=8192, input=4192, output=3000 | input changes from 4192 to 1192 (8192-4000-3000). Output stays 3000. |

## Технические детали

### 1. Обновление существующих тестов

#### TestOpenCodeConfig_Basic
- Assert `"output": 3000` — остаётся без изменений.
- Убрать жёсткий assert на конкретное значение `input` (зависит от context).
- Оставить assert на presence `"input"` и `"context"` keys.

#### TestOpenCodeConfig_WithMaxModelLen
- context=32768, buffer=4000, output=3000 → input=25768
- Изменить: `assert.Contains(t, string(body), `"input": 25768`)`

#### TestOpenCodeConfig_ContextLimitFromVLLM
- context=65536, buffer=4000, output=3000 → input=58536
- Изменить: `assert.Contains(t, string(body), `"input": 58536`)`

#### TestOpenCodeConfig_NoMaxModelLen
- context=8192, buffer=4000, output=3000 → input=1192
- Изменить: `assert.Contains(t, string(body), `"input": 1192`)`

### 2. Новые тесты

#### TestOpenCodeConfig_CustomInput
Проверяет, что при явном `OpenCodeContextInput > 0` формула `output = context - buffer - input` работает:

```go
func TestOpenCodeConfig_CustomInput(t *testing.T) {
    f := setupTest(t)
    f.addBackend(t, []string{"smart"}, func(w http.ResponseWriter, r *http.Request) {
        // Backend returning model with max_model_len=32768
        // ... (same handler as TestOpenCodeConfig_WithMaxModelLen)
    })

    // Set explicit input allocation
    cfg := f.store.Get()
    cfg.Global.OpenCodeContextInput = 8000
    cfg.Global.OpenCodeContextBuffer = 4000
    require.NoError(t, f.store.Set(cfg))

    f.start(t)
    defer f.cleanup()

    resp, body := f.do("GET", "/admin/opencode-config", nil)
    require.Equal(t, http.StatusOK, resp.StatusCode)

    // context=32768, buffer=4000, input=8000 → output=20768
    assert.Contains(t, string(body), `"context": 32768`)
    assert.Contains(t, string(body), `"input": 8000`)
    assert.Contains(t, string(body), `"output": 20768`)
}
```

#### TestOpenCodeConfig_CustomBuffer
Проверяет изменение buffer:

```go
func TestOpenCodeConfig_CustomBuffer(t *testing.T) {
    f := setupTest(t)
    f.addBackend(t, []string{"gpt-4"}, nil)

    cfg := f.store.Get()
    cfg.Global.OpenCodeContextBuffer = 2000
    require.NoError(t, f.store.Set(cfg))

    f.start(t)
    defer f.cleanup()

    resp, body := f.do("GET", "/admin/opencode-config", nil)
    require.Equal(t, http.StatusOK, resp.StatusCode)

    // context=8192, buffer=2000, auto input → output=3000, input=8192-2000-3000=3192
    assert.Contains(t, string(body), `"context": 8192`)
    assert.Contains(t, string(body), `"input": 3192`)
    assert.Contains(t, string(body), `"output": 3000`)
}
```

#### TestOpenCodeConfig_SmallContext
Проверяет guard-логику при малом context (auto mode):

```go
func TestOpenCodeConfig_SmallContext(t *testing.T) {
    f := setupTest(t)
    // Backend returning model with max_model_len=5000
    f.addBackend(t, []string{"small"}, func(w http.ResponseWriter, r *http.Request) {
        // ... max_model_len: 5000
    })

    f.start(t)
    defer f.cleanup()

    resp, body := f.do("GET", "/admin/opencode-config", nil)
    require.Equal(t, http.StatusOK, resp.StatusCode)

    // context=5000, buffer=4000, auto → output=3000, input=5000-4000-3000=-2000
    // Guard: input < 1000 → input=1000
    // Then: output = 5000-4000-1000 = 0
    // Guard: output < 3000 → output=3000
    // Final: input=1000, output=3000
    assert.Contains(t, string(body), `"context": 5000`)
    assert.Contains(t, string(body), `"input": 1000`)
    assert.Contains(t, string(body), `"output": 3000`)
}
```

#### TestOpenCodeConfig_SmallContextExplicit
Проверяет guard-логику при малом context + явный input (explicit mode).
Без этого теста guard на minimum output в explicit mode не покрывался.

```go
func TestOpenCodeConfig_SmallContextExplicit(t *testing.T) {
    f := setupTest(t)
    // Backend returning model with max_model_len=5000
    f.addBackend(t, []string{"small"}, func(w http.ResponseWriter, r *http.Request) {
        // ... max_model_len: 5000
    })

    // Set explicit input allocation
    cfg := f.store.Get()
    cfg.Global.OpenCodeContextInput = 3000  // explicit input
    cfg.Global.OpenCodeContextBuffer = 4000
    require.NoError(t, f.store.Set(cfg))

    f.start(t)
    defer f.cleanup()

    resp, body := f.do("GET", "/admin/opencode-config", nil)
    require.Equal(t, http.StatusOK, resp.StatusCode)

    // context=5000, buffer=4000, explicit input=3000
    // output = 5000 - 4000 - 3000 = -2000
    // Guard: output < 3000 → output=3000
    // Final: input=3000, output=3000 (sum > context, but guards prevent negative output)
    assert.Contains(t, string(body), `"context": 5000`)
    assert.Contains(t, string(body), `"input": 3000`)
    assert.Contains(t, string(body), `"output": 3000`)
}
```

#### TestOpenCodeConfig_ConfigValidation
Проверяет валидацию новых полей при Load:

```go
func TestOpenCodeConfig_ConfigValidation(t *testing.T) {
    f := setupTest(t)

    // Write invalid values to file via Set(), then verify Load() restores defaults.
    // Set() is needed because Load() reads from the file, not from memory.
    cfg := f.store.Get()
    cfg.Global.OpenCodeContextBuffer = -100
    cfg.Global.OpenCodeContextInput = -50
    require.NoError(t, f.store.Set(cfg)) // persists invalid values to file
    require.NoError(t, f.store.Load())   // reloads from file, restores defaults

    cfg = f.store.Get()
    assert.Equal(t, 4000, cfg.Global.OpenCodeContextBuffer)
    assert.Equal(t, 0, cfg.Global.OpenCodeContextInput)
}
```

### 3. Порядок обновления тестов

1. Сначала обновить существующие assert'ы на новые expected значения.
2. Добавить новые тесты.
3. Запустить `go test ./server/...` и убедиться, что все тесты проходят.

## Файлы для изменения

| Файл | Действие |
|------|----------|
| `server/server_test.go` | Обновление 4 существующих тестов + 5 новых тестов |

## Требования к юнит-тестам

- Все тесты должны проходить: `go test ./server/... -v`
- Покрытие new code path: auto mode (input=0), explicit mode (input>0), guard (small context auto + explicit), custom buffer.
- Тесты должны быть изолированными (каждый создаёт свой fixture).

## Критерии приёмки

1. Все существующие тесты `TestOpenCodeConfig_*` обновлены и проходят.
2. Добавлены тесты: `TestOpenCodeConfig_CustomInput`, `TestOpenCodeConfig_CustomBuffer`, `TestOpenCodeConfig_SmallContext`, `TestOpenCodeConfig_SmallContextExplicit`, `TestOpenCodeConfig_ConfigValidation`.
3. `go test ./server/...` проходит без ошибок.
4. `go test ./...` (весь проект) проходит без ошибок.
