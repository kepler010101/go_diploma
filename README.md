# Gophermart (GoFirmart)
Сервис накопления бонусов по заданию. Работает с куками, общается с внешним Accrual асинхронно.

## Запуск локально
1. Поднять PostgreSQL (пример на Docker):
`
docker run --name pg -e POSTGRES_PASSWORD=pass -p 5432:5432 -d postgres:16
`
2. Настроить переменные окружения или флаги (флаги важнее):
   - RUN_ADDRESS / -a (по умолчанию :8080)
   - DATABASE_URI / -d (пример: postgres://postgres:pass@localhost:5432/postgres?sslmode=disable)
   - ACCRUAL_SYSTEM_ADDRESS / -r (пример: http://localhost:8081)
   - ACCRUAL_POLL_INTERVAL / -p, секунды (по умолчанию 2)
   - ACCRUAL_WORKERS / -w (по умолчанию 3)
3. Запустить сервис:
`
go run ./cmd/gophermart -a :8080
`
4. Запустить accrual из каталога cmd/accrual или свой тестовый сервис.

## Слои
- handlers - HTTP и middleware (gzip для списков)
- service - бизнес-логика и фоновые воркеры Accrual
- repo - работа с БД и миграции
- cmd/gophermart - main, конфигурация, graceful shutdown

## Аутентификация
- HttpOnly cookie token. Регистрация и логин отдают cookie при ответе 200.
- Все пользовательские маршруты требуют cookie.

## Эндпоинты
- POST  /api/user/register - 200 400 409 500
- POST  /api/user/login - 200 400 401 500
- POST  /api/user/orders - 200 202 400 401 409 422 500 (тело text/plain)
- GET   /api/user/orders - 200 204 401 500 (сортировка uploaded_at DESC, поле accrual только у PROCESSED)
- GET   /api/user/balance - 200 401 500 -> { "current", "withdrawn" }
- POST  /api/user/balance/withdraw - 200 401 402 422 500 (транзакция с SELECT ... FOR UPDATE)
- GET   /api/user/withdrawals - 200 204 401 500 (сортировка processed_at DESC)

## Интеграция с Accrual
- Воркеры ходят на GET /api/orders/{number}.
- При 429 соблюдают Retry-After: пока таймер не закончился, задания не берут.
- Статусы - REGISTERED, PROCESSING, INVALID, PROCESSED.
- При PROCESSED баллы поступают на баланс один раз (точно идемпотентно).

## Gzip
- Включен только для GET /api/user/orders и GET /api/user/withdrawals.
- Нужен Accept-Encoding: gzip.
- Для ответов 204 заголовок Content-Encoding не ставится.

## Тесты и CI
- go test 
- Юнит-тесты покрывают аутентификацию, приём и выдачу заказов, воркеры (включая 429), баланс и списания.
- В GitHub Actions запускаются линтеры и тесты.