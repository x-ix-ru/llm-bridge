# Спецификация Task 002 — Рефактор handleOpenCodeConfig() на новую формулу

## Описание

Переписать логику расчёта `input` и `output` лимитов в `handleOpenCodeConfig()` (файл `server/opencode.go`) с учётом новых полей конфигурации и формулы `context = buffer + input + output`.

## Полный контекст

### Текущая логика (строки 86–91 opencode.go):

```go
ctx := extractMaxModelLen(details[name])
outLimit := defaultGenerationLimit      // 3000 (хардкод)
inLimit := ctx - 4000                    // 4000 (хардкод)
if inLimit < 1000 {
    inLimit = 1000
}
```

### Новая формула:

```
context = buffer + input + output
```

Отсюда: `output = context - buffer - input`.

### Значения по умолчанию:

- `buffer = 4000` (из `cfg.Global.OpenCodeContextBuffer`)
- `input = 0` означает auto → `input = context - buffer - defaultGenerationLimit`
- При auto: `output = defaultGenerationLimit = 3000` (backward compatible)

### При явном input > 0:

- `output = context - buffer - input`

## Технические детали

### 1. Чтение конфигурации

В начале `handleOpenCodeConfig()` добавить чтение конфига:

```go
cfg := s.cfg.Get()
buffer := cfg.Global.OpenCodeContextBuffer
inputAlloc := cfg.Global.OpenCodeContextInput
```

> **Важно**: текущий код уже читает `cfg` для `OpenCodeBaseURL` (строка 60). Нужно реорганизовать, чтобы не читать дважды.

### 2. Новый блок расчёта лимитов

Заменить строки 86–91 на:

```go
ctx := extractMaxModelLen(details[name])

buffer := cfg.Global.OpenCodeContextBuffer
// Note: buffer is already validated by Load()/Set() to be >= 0.
// If it's 0, treat as default.
if buffer <= 0 {
    buffer = 4000
}

var inLimit, outLimit int

if cfg.Global.OpenCodeContextInput > 0 {
    // Explicit input allocation: output is derived
    inLimit = cfg.Global.OpenCodeContextInput
    outLimit = ctx - buffer - inLimit
} else {
    // Auto mode (default): preserve output = defaultGenerationLimit
    outLimit = defaultGenerationLimit
    inLimit = ctx - buffer - outLimit
}

// Guards for small context windows — applied in BOTH modes
minInput := 1000
if inLimit < minInput {
    inLimit = minInput
    outLimit = ctx - buffer - inLimit
}
// Guard minimum output — applies to explicit mode too,
// preventing negative output when context < buffer + input
if outLimit < defaultGenerationLimit {
    outLimit = defaultGenerationLimit
}
```

### 3. Сохранение констант

- `defaultGenerationLimit = 3000` — оставляется как fallback minimum для output.
- `defaultMaxModelLen = 8192` — оставляется без изменений (default context).
- `defaultProviderName = "llm-bridge"` — без изменений.

### 4. Извлечение base_url

Текущий код читает `cfg` для `OpenCodeBaseURL` на строке 60. После рефакторинга `cfg` будет прочитан один раз в начале функции:

```go
cfg := s.cfg.Get()
baseURL := r.URL.Query().Get("base_url")
if baseURL == "" {
    baseURL = cfg.Global.OpenCodeBaseURL
}
// ... остальная логика base_url ...
```

### 5. Рефакторинг extractMaxModelLen

Функция `extractMaxModelLen` использует `defaultGenerationLimit` в ветке `meta.MaxTokens > 0` (строка 163):
```go
case meta.MaxTokens > 0:
    return meta.MaxTokens + defaultGenerationLimit
```
Оставить без изменений — эта ветка вычисляет context, а не output.

## Файлы для изменения

| Файл | Действие |
|------|----------|
| `server/opencode.go` | Рефакторинг `handleOpenCodeConfig()`: чтение конфига, новая формула, guards |

## Требования к юнит-тестам

Юнит-тесты покрываются в Task 003. В данной задаче — только реализация.

## Критерии приёмки

1. `handleOpenCodeConfig()` читает `OpenCodeContextBuffer` и `OpenCodeContextInput` из конфига.
2. При значениях по умолчанию (buffer=4000, input=0): `output = 3000`, `input = context - 4000 - 3000` (backward compatible output).
3. При явном `OpenCodeContextInput > 0`: `output = context - buffer - input`.
4. Guard: `input >= 1000` (min input).
5. Guard: `output >= defaultGenerationLimit` (min output = 3000).
6. Формула `context = buffer + input + output` выполняется (за исключением случаев, когда guards увеличивают sum — это допустимо).
7. Константы `defaultMaxModelLen` и `defaultProviderName` не изменены.
8. `extractMaxModelLen()` не изменён.
9. Код компилируется (`go build ./...` успешно).
10. Существующие тесты `TestOpenCodeConfig_*` могут сломаться (новые значения) — это ожидается и будет фиксировано в Task 003.
