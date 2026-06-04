# Спецификация Task 001 — Config fields for OpenCode context limits

## Описание

Добавить два поля в `GlobalConfig` (структура в `config/config.go`), которые управляют распределением контекстного окна при генерации OpenCode-конфигурации.

## Технические детали

### 1. Новые поля в `GlobalConfig`

Добавить после `OpenCodeBaseURL`:

```go
OpenCodeContextBuffer int    `yaml:"opencode_context_buffer,omitempty"`
OpenCodeContextInput  int    `yaml:"opencode_context_input,omitempty"`
```

### 2. Значения по умолчанию

В `DefaultConfig()`:
```go
OpenCodeContextBuffer: 4000,
OpenCodeContextInput:  0,
```

- `OpenCodeContextBuffer = 4000` — соответствует текущему хардкоду.
- `OpenCodeContextInput = 0` — значение 0 означает "auto": input вычисляется как `context - buffer - defaultGenerationLimit`, что даёт output = 3000 и обеспечивает backward compatibility.

### 3. Валидация при `Load()`

После unmarshal (в существующем блоке валидации `Load()`):

```go
if cfg.Global.OpenCodeContextBuffer <= 0 {
    cfg.Global.OpenCodeContextBuffer = 4000
}
// OpenCodeContextInput = 0 is valid (means auto), positive values are explicit allocation
if cfg.Global.OpenCodeContextInput < 0 {
    cfg.Global.OpenCodeContextInput = 0
}
```

### 4. Валидация при `Set()`

В методе `Set()` добавить проверку:

```go
if cfg.Global.OpenCodeContextBuffer < 0 {
    return fmt.Errorf("global: opencode_context_buffer must be >= 0")
}
if cfg.Global.OpenCodeContextInput < 0 {
    return fmt.Errorf("global: opencode_context_input must be >= 0")
}
```

### 5. Константы

Добавить константы в `config/config.go` (вверху файла):

```go
// Default OpenCode context buffer (tokens reserved as safety margin).
const DefaultOpenCodeContextBuffer = 4000

// Default OpenCode context input. 0 means auto-computed.
const DefaultOpenCodeContextInput = 0
```

## Файлы для изменения

| Файл | Действие |
|------|----------|
| `config/config.go` | Добавление полей, констант, defaults, валидации |

## Требования к юнит-тестам

Юнит-тесты для config покрываются в рамках Task 003 (интеграционные тесты).
В данной задаче тесты не требуются — только изменение структур и валидации.

## Критерии приёмки

1. `GlobalConfig` содержит два новых поля: `OpenCodeContextBuffer` (int) и `OpenCodeContextInput` (int).
2. YAML-теги полей: `opencode_context_buffer` и `opencode_context_input`.
3. `DefaultConfig()` возвращает `OpenCodeContextBuffer = 4000`, `OpenCodeContextInput = 0`.
4. `Load()` восстанавливает defaults при <= 0 (buffer) и < 0 (input).
5. `Set()` отклоняет отрицательные значения с ошибкой.
6. Код компилируется (`go build ./...` успешно).
