# Руководство по участию в проекте / Contributing Guide

---

## Настройка среды разработки / Development Environment Setup

### Требования / Requirements

- Go 1.26.3
- Docker (для сборки образа / for building the Docker image)

### Первый запуск / First Run

```bash
# Клонируйте репозиторий / Clone the repository
git clone <repository-url>
cd llm-bridge

# Загрузите зависимости / Download dependencies
go mod download

# Соберите проект / Build the project
go build -o llm-bridge ./cmd/llm-bridge
```

### Сборка Docker-образа / Docker Build

```bash
docker build -t llm-bridge .
```

---

## Запуск тестов / Running Tests

Запустите все тесты в проекте:

```bash
go test ./...
```

В проекте более 100 тестов, охватывающих все пакеты. Все пакеты содержат файлы `_test.go`.

### Пример запуска тестов конкретного пакета / Run Tests for a Specific Package

```bash
go test ./server
go test ./discovery
go test ./metrics
```

Для вывода подробной информации:

```bash
go test -v ./...
```

---

## Структура проекта / Project Structure

```
cmd/llm-bridge/main.go    — точка входа приложения / application entry point
config/                   — парсинг YAML-конфигурации и её сохранение / YAML config parsing and persistence
backend/                  — пул серверов-бэкендов, HTTP-клиенты, прокси / backend server pool, HTTP clients, proxy
discovery/                — обнаружение моделей через периодический опрос /v1/models / model discovery via periodic /v1/models polling
router/                   — алгоритм выбора сервера (расстояние + round-robin) / server selection algorithm (distance + round-robin)
metrics/                  — сбор метрик Prometheus из vLLM / Prometheus metrics collection from vLLM
server/                   — HTTP-сервер, маршруты, обработчики / HTTP server, routes, handlers
web/static/               — веб-интерфейс администратора (HTML/CSS/JS), встроен в бинарь / Admin UI (HTML/CSS/JS, embedded)
```

### Подробное описание пакетов / Package Descriptions

| Пакет / Package | Назначение / Purpose |
|-----------------|----------------------|
| `config` | Чтение и валидация YAML-конфигурации, сохранение изменённых настроек / Reads and validates YAML config, persists changed settings |
| `backend` | Управление пулом vLLM-серверов, HTTP-клиент с кешированием, обратный прокси / Manages vLLM server pool, HTTP client with caching, reverse proxy |
| `discovery` | Периодический опрос `/v1/models` на каждом бэкенде, актуализация кэша моделей / Periodic polling `/v1/models` on each backend, model cache refresh |
| `router` | Стратегия маршрутизации: выбор сервера по расстоянию + round-robin / Routing strategy: server selection by distance + round-robin |
| `metrics` | Агрегация метрик из vLLM, экспорт в Prometheus-формате / Aggregates vLLM metrics, exports in Prometheus format |
| `server` | HTTP-сервер, регистрация маршрутов, обработчики API и статических файлов / HTTP server, route registration, API and static file handlers |

---

## Правила написания кода / Coding Conventions

### Форматирование / Formatting

- Используйте `gofmt` для форматирования кода. Все изменения должны проходить проверку:

  ```bash
  gofmt -l .
  ```

### Документирование / Documentation

- Все публичные типы и функции должны иметь комментарии на уровне пакета / All public types and functions must have package-level doc comments.

### Контекст / Context

- Контекст должен передаваться через все слои приложения / Context must be propagated through all layers.

### Обработка ошибок / Error Handling

- Используйте оборачивание ошибок с указанием контекста действия:

  ```go
  return fmt.Errorf("fetching models: %w", err)
  ```

### Параллелизм / Concurrency

- Для общих счётчиков используйте атомарные операции (`sync/atomic`) / Use atomic operations (`sync/atomic`) for shared counters.
- Для доступа к общему состоянию используйте `RWMutex` / Use `RWMutex` for shared state access.

### Зависимости / Dependencies

Проект использует следующие зависимости:

- `github.com/go-chi/chi/v5` v5.3.0 — HTTP-маршрутизатор / HTTP router
- `github.com/stretchr/testify` v1.11.1 — тестовые ассерты / test assertions
- `gopkg.in/yaml.v3` v3.0.1 — парсинг YAML / YAML parsing

---

## Вклад в проект / Contributing

### Процесс отправки PR / PR Process

1. **Создайте ветку / Create a branch**

   ```bash
   git checkout -b feature/your-feature-name
   ```

2. **Внесите изменения / Make your changes**

   Следуйте правилам кодирования, описанным выше / Follow the coding conventions described above.

3. **Напишите / обновите тесты / Write / update tests**

   Все изменения должны покрываться тестами / All changes must be covered by tests:

   ```bash
   go test ./...
   ```

4. **Коммит / Commit**

   Пишите осмысленные сообщения коммитов / Write meaningful commit messages:

   ```bash
   git commit -m "add: implement model discovery polling"
   ```

5. **Отправьте ветку / Push and create PR**

   ```bash
   git push origin feature/your-feature-name
   ```

   Откройте pull request в репозитории / Open a pull request in the repository.

### Checklist перед отправкой / Pre-submission Checklist

- [ ] Тесты проходят / Tests pass (`go test ./...`)
- [ ] Код отформатирован / Code is formatted (`gofmt`)
- [ ] Публичные типы и функции документированы / Public types and functions are documented
- [ ] Контекст передаётся через все слои / Context is propagated through all layers
- [ ] Нет гонок данных / No data races (`go test -race ./...`)

---

## Структура тестов / Test Structure

| Файл / File | Кол-во тестов / # Tests | Тип / Type |
|-------------|-------------------------|------------|
| `server/server_test.go` | ~56 | интеграционные + юнит / integration + unit |
| `discovery/discovery_test.go` | 12 | unit |
| `metrics/metrics_test.go` | 11 | unit |
| `router/router_test.go` | 9 | unit |
| `config/config_test.go` | 9 | unit |
| `backend/backend_test.go` | 7 | unit |

Тесты используют `testify/assert` для ассертов и чистый Go-testing (`go test`).

---

## Получение помощи / Getting Help

Если у вас возникли вопросы, создайте issue в репозитории / If you have questions, create an issue in the repository.
