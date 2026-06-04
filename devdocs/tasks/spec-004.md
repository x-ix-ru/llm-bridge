# Спецификация Task 004 — Документация новых полей конфигурации

## Описание

Добавить документацию для `opencode_context_buffer` и `opencode_context_input` в существующий файл `docs/configuration.md`.

## Полный контекст

Текущий `docs/configuration.md` содержит секции для всех полей `GlobalConfig` в разделе "Global Settings". Новые поля нужно добавить после `opencode_base_url`.

## Технические детали

### 1. Обновление Table of Contents

Добавить в TOC после `opencode_base_url`:

```markdown
- [opencode_context_buffer / Буфер контекста OpenCode](#opencode_context_buffer--буфер-контекста-opencode)
- [opencode_context_input / Аллокация input OpenCode](#opencode_context_input--аллокация-input-opencode)
```

### 2. Новая секция: opencode_context_buffer

Поместить после секции `opencode_base_url`:

```markdown
### opencode_context_buffer / Буфер контекста OpenCode

Type / Тип: `int`
Default / По умолчанию: `4000`

Buffer of tokens reserved in the context window when generating `opencode.jsonc` configuration. This buffer acts as a safety margin between input and output token allocations.
Буфер токенов, резервируемый в контекстном окне при генерации конфигурации `opencode.jsonc`. Выступает в роли запаса безопасности между аллокацией input и output токенов.

The context window is split as: `context = buffer + input + output`.
Контекстное окно распределяется по формуле: `context = buffer + input + output`.

---
```

### 3. Новая секция: opencode_context_input

```markdown
### opencode_context_input / Аллокация input OpenCode

Type / Тип: `int`
Default / По умолчанию: `0` (auto)

Token allocation for model input in the generated `opencode.jsonc` configuration. A value of `0` means automatic calculation: `input = context - buffer - 3000`, which keeps output at 3000 tokens (backward compatible).
Аллокация токенов на input модели в сгенерированной конфигурации `opencode.jsonc`. Значение `0` означает автоматический расчёт: `input = context - buffer - 3000`, что сохраняет output на уровне 3000 токенов (backward compatible).

When set to a positive value, output is derived: `output = context - buffer - input`.
При положительном значении output вычисляется: `output = context - buffer - input`.

#### Examples / Примеры

```yaml
# Default: auto input, output = 3000 / По умолчанию: auto input, output = 3000
global:
    opencode_context_buffer: 4000
    opencode_context_input: 0

# Explicit input allocation / Явная аллокация input
global:
    opencode_context_buffer: 4000
    opencode_context_input: 8000
    # For a model with context=32768: output = 32768 - 4000 - 8000 = 20768
```

#### Guards / Ограничения

- Minimum input: `1000` tokens / Минимальный input: `1000` токенов
- Minimum output: `3000` tokens / Минимальный output: `3000` токенов
- For very small context windows, guards may cause `buffer + input + output > context`.
  Для очень малых контекстных окон ограничители могут привести к `buffer + input + output > context`.

---
```

### 4. Обновление примеров конфигураций

Во всех YAML-примерах внизу документа (`## Example Configurations`) добавить строки:

```yaml
global:
    # ... existing fields ...
    opencode_base_url: "http://localhost:8080"
    opencode_context_buffer: 4000
    opencode_context_input: 0
```

### 5. Обновление example config

В секции "Config File Structure" добавить новые поля в пример.

## Файлы для изменения

| Файл | Действие |
|------|----------|
| `docs/configuration.md` | TOC + 2 новые секции + обновление примеров |

## Требования к юнит-тестам

Не применимо — задача по документации.

## Критерии приёмки

1. `docs/configuration.md` содержит секции для `opencode_context_buffer` и `opencode_context_input`.
2. Table of Contents обновлён.
3. Все YAML-примеры в документе включают новые поля.
4. Формула `context = buffer + input + output` описана.
5. Документация на двух языках (RU/EN) как в существующем стиле документа.
6. Примеры значений корректны (проверены вручную).
