# Telegram tracker

Многопользовательский бот для коротких check-in: настроение, стресс, тяга. Данные в PostgreSQL, деплой только через Docker Compose.

## Запуск

```bash
cp .env.example .env
# заполнить TELEGRAM_BOT_TOKEN, POSTGRES_PASSWORD и DATABASE_URL
mkdir -p data/backups
docker compose up -d --build
docker compose logs -f bot
```

Миграции схемы применяются при старте бота (golang-migrate, файлы в `migrations/postgres/`).

Остановка: `docker compose stop`. Данные Postgres живут в named volume `postgres_data`, дампы — в `./data/backups`.

## Команды бота

`/start` `/checkin` `/today` `/history` `/edit` `/stats` `/cancel`
