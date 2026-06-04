# Task 001 — Добавить поля конфигурации OpenCode context limits

## Описание

Добавить два новых поля в `GlobalConfig` для управления лимитами контекстного окна при генерации `opencode.jsonc`:
- `OpenCodeContextBuffer` — буфер, резервируемый в контекстном окне (default: 4000).
- `OpenCodeContextInput` — аллокация токенов на input (default: 0 = auto).

Эти поля заменяют хардкод-константы в `server/opencode.go`.

## Покрытие требований

- Перевод хардкода `4000` (buffer) и `3000` (output) в конфигурацию.
- Подготовка для новой формулы `context = buffer + input + output`.

## Файлы для изменения

- `config/config.go` — добавить поля, defaults, валидацию при Load/Set.
