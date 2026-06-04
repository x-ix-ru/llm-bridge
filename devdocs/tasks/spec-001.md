# Spec 001 — ENV override helpers и логика переопределения

## Описание

Добавить в `config/config.go` два helper-функции и логику применения ENV-переменных в `Store.Load()`.

### envInt(key string, defaultVal int) int

- Читает `os.Getenv(key)` (case-sensitive, верхний регистр).
- Если ENV пустой — возвращает `defaultVal`.
- Если ENV установлен, парсит через `strconv.Atoi()`.
- Если парсинг не удался — возвращает `defaultVal` (не паникует).

### envString(key string, defaultVal string) string

- Читает `os.Getenv(key)`.
- Если ENV пустой — возвращает `defaultVal`.
- Если ENV установлен — возвращает значение ENV.

### applyEnvOverrides(cfg *GlobalConfig)

Приватная функция, которая применяет ENV override ко всем полям `GlobalConfig`:

```go
func applyEnvOverrides(cfg *GlobalConfig) {
    cfg.FallbackStrategy = FallbackStrategy(
        envString("FALLBACK_STRATEGY", string(cfg.FallbackStrategy)),
    )
    // валидация: если ENV дал невалидную стратегию, оставляем старое значение
    if !cfg.FallbackStrategy.Valid() {
        cfg.FallbackStrategy = FallbackError
    }

    cfg.DiscoveryIntervalSec = envInt("DISCOVERY_INTERVAL_SEC", cfg.DiscoveryIntervalSec)
    if cfg.DiscoveryIntervalSec <= 0 {
        cfg.DiscoveryIntervalSec = 15
    }

    cfg.RequestTimeoutSec = envInt("REQUEST_TIMEOUT_SEC", cfg.RequestTimeoutSec)
    if cfg.RequestTimeoutSec <= 0 {
        cfg.RequestTimeoutSec = 60
    }

    cfg.QueueTimeoutSec = envInt("QUEUE_TIMEOUT_SEC", cfg.QueueTimeoutSec)
    if cfg.QueueTimeoutSec <= 0 {
        cfg.QueueTimeoutSec = 30
    }

    cfg.DrainTimeoutSec = envInt("DRAIN_TIMEOUT_SEC", cfg.DrainTimeoutSec)
    if cfg.DrainTimeoutSec <= 0 {
        cfg.DrainTimeoutSec = 30
    }

    cfg.ShutdownTimeoutSec = envInt("SHUTDOWN_TIMEOUT_SEC", cfg.ShutdownTimeoutSec)
    if cfg.ShutdownTimeoutSec <= 0 {
        cfg.ShutdownTimeoutSec = 10
    }

    // OpenCodeBaseURL: если ENV не пустой, переопределяем; иначе оставляем как есть
    if v := os.Getenv("OPENCODE_BASE_URL"); v != "" {
        cfg.OpenCodeBaseURL = v
    }

    cfg.OpenCodeContextBuffer = envInt("OPENCODE_CONTEXT_BUFFER", cfg.OpenCodeContextBuffer)
    if cfg.OpenCodeContextBuffer <= 0 {
        cfg.OpenCodeContextBuffer = 4000
    }

    cfg.OpenCodeContextInput = envInt("OPENCODE_CONTEXT_INPUT", cfg.OpenCodeContextInput)
    if cfg.OpenCodeContextInput < 0 {
        cfg.OpenCodeContextInput = 0
    }
}
```

### Изменение Store.Load()

В метод `Store.Load()`, после YAML парсинга и валидации, перед присваиванием `s.config = cfg`, вызвать `applyEnvOverrides(&cfg.Global)`.

```
Текущий порядок:
  1. Read YAML file
  2. Unmarshal YAML
  3. Validate fields (fallback_strategy, timeouts)
  4. s.config = cfg

Новый порядок:
  1. Read YAML file
  2. Unmarshal YAML
  3. Validate fields (fallback_strategy, timeouts)
  4. applyEnvOverrides(&cfg.Global)  <-- NEW
  5. s.config = cfg
```

## Технические детали

- ENV-переменные: case-sensitive, верхний регистр (`DISCOVERY_INTERVAL_SEC`, не `discovery_interval_sec`).
- ENV пустой (`""`) ≠ ENV не установлен. В обоих случаях `os.Getenv()` возвращает `""`, используется default/YAML.
- Невалидные ENV-значения:
  - `envInt` не смог распарсить → defaultVal (YAML значение, которое уже прошло валидацию).
  - `FALLBACK_STRATEGY` дал невалидную строку → `FallbackError`.
- Не нужно вызывать `s.saveLocked()` — ENV-переопределения не записываются обратно в YAML. Это runtime override.

## Файлы для изменения

- `config/config.go` — добавление helpers, `applyEnvOverrides()`, изменение `Store.Load()`.

## Требования к юнит-тестам

См. Spec 004. Тесты в отдельной задаче.

## Критерии приёмки

- `envInt()` возвращает default при пустом ENV.
- `envInt()` возвращает распаршенное значение при корректном ENV.
- `envInt()` возвращает default при невалидном ENV ("abc").
- `envString()` возвращает default при пустом ENV.
- `envString()` возвращает значение ENV при установленном ENV.
- `Store.Load()` применяет ENV override поверх YAML.
- При отсутствии ENV — YAML-значение сохраняется без изменений.
- Невалидный `FALLBACK_STRATEGY` ENV → `FallbackError`.
- Отрицательный int из ENV → default.
- `go build ./...` без ошибок.
