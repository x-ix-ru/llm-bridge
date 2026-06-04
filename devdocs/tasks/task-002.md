# Task 002 — Рефактор handleOpenCodeConfig() на новую формулу

## Описание

Заменить хардкод-формулу расчёта лимитов в `handleOpenCodeConfig()` на новую формулу `output = context - buffer - input`, используя значения из конфигурации (`GlobalConfig.OpenCodeContextBuffer`, `GlobalConfig.OpenCodeContextInput`).

## Покрытие требований

- Удаление хардкода `defaultGenerationLimit = 3000` и `buffer = 4000` из логики расчёта.
- Реализация формулы `context = buffer + input + output`.
- Чтение buffer и input из конфигурации.
- Guard-логика для малых context windows.

## Файлы для изменения

- `server/opencode.go` — переписать блок расчёта `inLimit` / `outLimit` в цикле по моделям.
