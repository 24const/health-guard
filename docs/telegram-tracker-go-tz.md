# Техническое задание: Telegram-бот для отслеживания состояния и тяги (Go)

## 1. Цель

Разработать Telegram-бота на Go для быстрого регулярного отслеживания эмоционального состояния, уровня стресса и тяги к курению и алкоголю.

Бот несколько раз в день инициирует короткий check-in, сохраняет ответы и позволяет анализировать динамику состояния, триггеры и эффективность способов справляться с тягой.

Дополнительно бот умеет показывать прошлые записи и редактировать их, включая дозаполнение пропущенных дней задним числом.

Бот многопользовательский: любой Telegram-пользователь может начать им пользоваться, данные каждого хранятся и показываются отдельно. Разворачивается в Docker на VPS.

## 2. Технологический стек

Зафиксированный стек (без "или/или"):

* Go 1.26+
* Telegram Bot API, long polling
* Библиотека Telegram: `gopkg.in/telebot.v3`
* PostgreSQL 16
* Драйвер БД: `github.com/jackc/pgx/v5` (чистый Go, без CGO). Мигратор открывает `*sql.DB` через `pgx/v5/stdlib`, приложение работает через `pgxpool`
* Миграции схемы: `github.com/golang-migrate/migrate/v4` (`database/postgres` + `source/iofs`), SQL-файлы, применяются при старте бота
* Планировщик суточных заданий: `github.com/go-co-op/gocron/v2`
* Одноразовые задания (follow-up, snooze): собственная таблица `jobs` + goroutine-поллер
* Конфигурация: переменные окружения (`.env` через `env_file` в Compose)
* Логирование: стандартный `log/slog` в stdout/stderr
* Часовые пояса: `import _ "time/tzdata"` в `main` (чтобы `TIMEZONE` работал в контейнере)
* Деплой: Docker (multi-stage) + Docker Compose (сервисы `postgres`, `bot`, `backup`)

Почему так:

* `pgx` даёт `CGO_ENABLED=0` и статичный бинарь. Образ бота собирается без gcc.
* follow-up хранится в БД, а не только в памяти планировщика, поэтому переживает рестарт контейнера.
* golang-migrate версионирует схему отдельно от кода: каждый шаг — пара `.up.sql` / `.down.sql`, таблица версий `schema_migrations`. Автомиграции ORM нет.
* Docker Compose поднимает бота и Postgres вместе: `docker compose up -d` на VPS, данные Postgres в named volume, бэкапы в bind-mount.

Альтернатива, если понадобится: библиотека `github.com/go-telegram/bot` (без зависимостей). GORM AutoMigrate и ручной `CREATE TABLE` в коде не использовать.

Для MVP не нужны: домен, HTTPS-терминация, webhook, nginx, отдельный backend API, frontend. Опубликованные порты контейнеров не нужны: бот ходит в Telegram API исходящим HTTPS, Postgres слушает только внутри docker-сети.

Бот и база работают только через Docker Compose (`restart: unless-stopped`). Другого способа деплоя нет.

## 3. Конфигурация

Все настройки через переменные окружения. Токен не хранится в коде.

Минимальный набор:

```env
TELEGRAM_BOT_TOKEN=
TIMEZONE=Europe/Belgrade
POSTGRES_USER=bot
POSTGRES_PASSWORD=
POSTGRES_DB=bot
DATABASE_URL=postgres://bot:PASSWORD@postgres:5432/bot?sslmode=disable
CHECKIN_TIMES=09:00,13:00,18:00,21:30
EVENING_REVIEW_TIME=22:30
FOLLOWUP_DELAY_MIN=20
SNOOZE_DELAY_MIN=15
JOB_POLL_INTERVAL_SEC=20
JOB_GRACE_MIN=120
BACKUP_DIR=/backups
BACKUP_KEEP=14
MIGRATION_DIRECTION=up
MIGRATION_VERSION=0
DB_MAX_OPEN_CONNS=10
DB_MAX_IDLE_CONNS=5
DB_CONN_MAX_LIFETIME=1h
DB_CONN_MAX_IDLE_TIME=1h
```

Список пользователей не конфигурируется: бот работает для всех, кто ему написал, и разделяет данные по `user_id`.

`TIMEZONE`, `CHECKIN_TIMES` и `EVENING_REVIEW_TIME` общие для всех пользователей. Персональное расписание и персональный часовой пояс в MVP не поддерживаются, это следующий этап.

`JOB_GRACE_MIN` определяет, за сколько минут просроченное после рестарта задание ещё имеет смысл выполнить. Более старые задания помечаются просроченными и не выполняются, чтобы не сыпать напоминаниями пачкой после долгого простоя.

`DATABASE_URL` — единственная точка подключения бота к Postgres. Хост `postgres` это имя сервиса в Compose. `sslmode=disable` допустим только во внутренней docker-сети, наружу порт 5432 не публикуется.

`MIGRATION_DIRECTION` по умолчанию `up`. Полный `down` не поддерживается: откат только через `step_back` или `to_version` вместе с `MIGRATION_VERSION`. `DB_*` задают пул `pgxpool`.

Конфиг читается из `.env` и подаётся в сервисы через `env_file`. Данные Postgres живут в named volume `postgres_data`. Дампы `pg_dump` пишутся в bind-mount `./data/backups` на хосте.

## 4. Основной пользовательский сценарий

Бот 2–4 раза в день предлагает пройти check-in.

Пример:

> Как ты сейчас?

Пользователь отвечает на короткую последовательность вопросов, в основном через inline-кнопки. Один check-in занимает примерно 20–40 секунд. После прохождения все ответы сохраняются одной записью состояния.

## 5. Стандартный check-in

### 5.1. Настроение

> Как ты себя сейчас чувствуешь?

* 😫 Очень плохо
* 😕 Плохо
* 😐 Нормально
* 🙂 Хорошо
* 😄 Отлично

Хранить число от 1 до 5.

### 5.2. Напряжение

> Уровень напряжения сейчас?

Ответ 0–10, inline-кнопки.

### 5.3. Раздражение

> Насколько ты сейчас раздражён?

Ответ 0–10.

### 5.4. Тяга к курению

> Насколько сейчас хочется курить?

Ответ 0–10.

### 5.5. Тяга к алкоголю

> Насколько сейчас хочется выпить?

Ответ 0–10.

### 5.6. Удовольствие от жизни

> Насколько сейчас жизнь ощущается приятной или наполненной?

Ответ 0–10.

## 6. Дополнительные вопросы при высокой тяге

Если `smoking_craving >= 6` или `alcohol_craving >= 6`, бот задаёт три дополнительных вопроса.

### 6.1. Реальная потребность

> Чего тебе сейчас больше всего хочется на самом деле?

Отдохнуть, Побыть одному, Снять стресс, Переключиться, Развлечься, Получить удовольствие, Поесть, Поспать, Не знаю, Другое.

Один основной вариант.

### 6.2. Контекст

> Что сейчас происходит?

Работаю, Перерыв, Иду / еду, Дома, Общаюсь, Конфликт, Отдыхаю, Перед сном, Другое.

### 6.3. Способ справиться

> Что попробуешь сделать вместо сигареты или алкоголя?

Прогуляться, Подышать, Выпить воды, Выпить чай / лимонад, Побыть одному, Поесть, Душ, Переключиться на другую задачу, Ничего, Другое.

## 7. Повторная оценка тяги (follow-up)

Если при check-in зафиксирована высокая тяга (`>= 6`), бот планирует повторный короткий вопрос через `FOLLOWUP_DELAY_MIN` (по умолчанию 20 минут).

> Прошло немного времени. Насколько сейчас хочется курить?

или про алкоголь. Если высокими были обе тяги, спрашиваются обе.

Follow-up связывается с исходным check-in через `checkin_id`. Это даёт анализ изменения тяги:

```text
8 → 5
9 → 3
7 → 7
```

Важно для надёжности: follow-up не живёт в памяти планировщика. При планировании создаётся строка в таблице `jobs` со временем срабатывания в UTC. Goroutine-поллер раз в `JOB_POLL_INTERVAL_SEC` секунд выбирает готовые задания. При старте бота незакрытые задания подхватываются из БД. Так follow-up не теряется при рестарте процесса.

## 8. Расписание

Автоматические check-in несколько раз в день по фиксированному расписанию из `CHECKIN_TIMES`. Все времена трактуются в `TIMEZONE` (Europe/Belgrade).

Суточные времена регистрируются в `gocron` при старте. `gocron` пересчитывает ближайшее срабатывание, поэтому рестарт не ломает расписание.

В `gocron` регистрируется одно задание на каждое время из `CHECKIN_TIMES`, а не задание на пользователя. При срабатывании задание выбирает активных пользователей из `users` и рассылает приглашение каждому. Так число задач планировщика не растёт вместе с числом пользователей.

Рассылка идёт последовательно с ограничением частоты (Telegram режет массовую отправку). Ошибка доставки одному пользователю не должна прерывать рассылку остальным.

Количество и значения check-in меняются в конфигурации без правки основной логики.

Одноразовые задания (follow-up, snooze) идут не через `gocron`, а через таблицу `jobs`, потому что им нужна персистентность и произвольное время срабатывания.

## 9. Поведение уведомления

В назначенное время бот не запускает сразу длинную цепочку. Первое сообщение:

> Время короткого check-in. Как ты?

Кнопки:

* Начать
* Через 15 минут
* Пропустить

**Начать** запускает check-in.
**Через 15 минут** создаёт job на повторное предложение через `SNOOZE_DELAY_MIN`.
**Пропустить** сохраняет факт пропуска (запись со `status = skipped`).

Если Telegram отвечает, что пользователь заблокировал бота или чат недоступен, пользователь помечается `is_active = false` и больше не получает автоматические сообщения. Любая новая команда от него снова делает его активным.

## 10. Ручной check-in

Пользователь может начать check-in сам в любой момент.

Команда `/checkin`, а также постоянная reply-кнопка:

```text
📝 Check-in
```

Ручной check-in использует ту же логику и формат хранения, что и автоматический, отличается только `source = manual`.

## 11. Вечерний итог

Один раз вечером (`EVENING_REVIEW_TIME`) бот предлагает короткий итог дня.

* Максимальная тяга к курению, 0–10
* Максимальная тяга к алкоголю, 0–10
* Самый сложный момент: Утро, Работа, Перерыв, Дорога домой, Вечер дома, Конфликт, Одиночество, Усталость, Другое
* Что сегодня помогло больше всего: выбор из стандартных способов справиться либо свободный текст
* Курил сегодня: Нет / Да
* Пил алкоголь сегодня: Нет / Да

Запись в `daily_reviews` делается через upsert по паре `(user_id, local_date)`, поэтому повтор итога за тот же день не создаёт дубль, а обновляет запись.

Интерфейс не использует формулировок вроде "день провален" и не обнуляет прогресс после ответа "Да". Данные нужны для наблюдения, а не для оценки пользователя.

## 12. Просмотр и редактирование прошлых записей

Ключевое отличие от исходного ТЗ. Бот умеет показывать историю и менять уже сохранённые значения, включая дозаполнение пропущенного дня задним числом.

### 12.1. История

Команда `/history [N]` показывает последние N дней (по умолчанию 7). По каждому дню:

```text
Пн 17.08   check-in: 3   итог: заполнен   🚬 нет   🍷 нет
Вт 18.08   check-in: 2   итог: не заполнен
```

Каждый день это inline-кнопка. День определяется по `local_date`, а не по UTC.

### 12.2. Экран дня

По нажатию на день открывается детальный экран:

* сводка по check-in этого дня;
* состояние итога дня (заполнен или нет);
* кнопка **Заполнить / изменить итог дня**;
* список check-in этого дня как кнопки.

Кнопка итога запускает флоу вечернего итога, но целевой датой становится выбранный день. Запись пишется upsert-ом по `(user_id, local_date)`, поэтому пропущенный вчерашний итог просто заполняется сегодня.

### 12.3. Редактирование check-in

По нажатию на конкретный check-in открывается его детальный экран со всеми значениями и кнопкой у каждого редактируемого поля (настроение, стресс, раздражение, тяга к курению, тяга к алкоголю, удовольствие, потребность, контекст, способ справиться).

Нажатие на поле показывает ту же клавиатуру, что и при первичном вводе. Выбор значения делает `UPDATE` строки и проставляет `updated_at`. Дубликаты не создаются, правится существующая запись.

### 12.4. Команда быстрого доступа

`/edit` это алиас входа в историю за 7 дней. Дальше навигация кнопками.

### 12.5. Влияние на метрики

Так как итог дня теперь можно дозаполнить и поправить, метрики "дней без сигарет" и "дней без алкоголя" перестают зависеть от того, ответил ли пользователь именно вечером. Это чинит слабое место исходного ТЗ, где самая мотивирующая метрика висела на самом ненадёжном шаге.

## 13. Команды

```text
/start
/checkin
/today
/history
/edit
/stats
/cancel
```

* `/start`, краткое описание и регистрация пользователя (upsert в `users`, `is_active = true`).
* `/checkin`, ручной check-in.
* `/today`, показатели за текущий день.
* `/history [N]`, история за N дней с возможностью открыть и править день.
* `/edit`, вход в редактирование за последние 7 дней.
* `/stats`, статистика за 7 дней.
* `/cancel`, прервать текущий незавершённый диалог.

Пример `/today`:

```text
Сегодня:

Check-in: 3

Средняя тяга к курению: 4.7
Максимальная: 8

Средняя тяга к алкоголю: 2.3
Максимальная: 4

Средний стресс: 6.0
Среднее раздражение: 5.3
```

## 14. База данных

PostgreSQL 16. Подключение через `pgxpool`. Пул задаётся переменными `DB_*`.

Схема живёт только в SQL-миграциях golang-migrate. При старте бот:

1. открывает `*sql.DB` по `DATABASE_URL` через `pgx/v5/stdlib`;
2. применяет миграции (`MIGRATION_DIRECTION`, по умолчанию `up`);
3. закрывает соединение мигратора, поднимает `pgxpool`;
4. только после успешного `up` поднимает планировщик и long polling.

Если миграция падает или состояние `schema_migrations` dirty, процесс выходит с ошибкой, бот не стартует на старой или частично обновлённой схеме.

Правила миграций:

* инструмент один: `golang-migrate/v4`, директория `migrations/postgres/`;
* файлы нумеруются вперёд без дыр: `000001_init.up.sql` / `000001_init.down.sql`;
* у каждой миграции есть `down`, либо она явно помечена irreversible с пояснением;
* таблица версий `schema_migrations`; dirty-состояние прерывает старт с указанием версии;
* `direction=up` по умолчанию; полный `down` не поддерживается, откат только `step_back` или `to_version`;
* SQL встроен в бинарь через `embed.FS` (`migrations/migrations.go`), отдельный контейнер-мигратор не нужен;
* в SQL: `CREATE TABLE IF NOT EXISTS` / `CREATE INDEX IF NOT EXISTS`; `down` сначала дропает индексы, затем таблицу;
* GORM AutoMigrate и `CREATE TABLE` в Go-коде запрещены;
* пароль из `DATABASE_URL` не логировать.

Все временные метки — `TIMESTAMPTZ` в UTC. Дополнительно у записей есть `local_date` (`DATE` в `TIMEZONE`) для группировки по дням. Вся логика "какой это день" опирается на `local_date`, а `jobs.fire_at` хранится в UTC.

Upsert итога дня: `INSERT ... ON CONFLICT (user_id, local_date) DO UPDATE`.

Данные многопользовательские. `user_id` везде это Telegram user id. Любой запрос на чтение, обновление и агрегацию обязан фильтровать по `user_id` текущего пользователя, включая выборки по `id` записи: перед `UPDATE` check-in проверяется, что строка принадлежит этому пользователю. Иначе подобранный `callback_data` с чужим id даст доступ к чужой записи.

Поллер jobs берёт готовые строки так: `status = 'pending' AND fire_at <= NOW()` плюс `FOR UPDATE SKIP LOCKED`. Просроченное дольше `JOB_GRACE_MIN` помечается `expired` и не выполняется.

Стартовая миграция `000001_init.up.sql` создаёт таблицы ниже.

### Таблица `users`

```sql
CREATE TABLE users (
    id          BIGINT PRIMARY KEY,      -- Telegram user id
    username    TEXT,
    first_name  TEXT,
    is_active   BOOLEAN NOT NULL DEFAULT TRUE,
    created_at  TIMESTAMPTZ NOT NULL,
    updated_at  TIMESTAMPTZ
);
```

Пользователь заводится при первом сообщении боту (upsert по `id`). `is_active = false` означает, что бот заблокирован или чат недоступен: такому пользователю рассылка не идёт, его данные сохраняются.

Остальные таблицы ссылаются на `users(id)` через `user_id`.

### Таблица `checkins`

```sql
CREATE TABLE checkins (
    id              BIGSERIAL PRIMARY KEY,
    user_id         BIGINT NOT NULL REFERENCES users (id),
    created_at      TIMESTAMPTZ NOT NULL,
    local_date      DATE NOT NULL,
    updated_at      TIMESTAMPTZ,

    mood            SMALLINT,
    stress          SMALLINT,
    irritation      SMALLINT,

    smoking_craving SMALLINT,
    alcohol_craving SMALLINT,
    life_enjoyment  SMALLINT,

    need            TEXT,
    context         TEXT,
    coping_action   TEXT,

    source          TEXT NOT NULL,      -- scheduled | manual
    status          TEXT NOT NULL DEFAULT 'completed'  -- completed | skipped
);

CREATE INDEX idx_checkins_day ON checkins (user_id, local_date);
```

### Таблица `craving_followups`

```sql
CREATE TABLE craving_followups (
    id              BIGSERIAL PRIMARY KEY,
    checkin_id      BIGINT NOT NULL REFERENCES checkins (id),
    created_at      TIMESTAMPTZ NOT NULL,
    updated_at      TIMESTAMPTZ,

    smoking_craving SMALLINT,
    alcohol_craving SMALLINT
);
```

### Таблица `daily_reviews`

```sql
CREATE TABLE daily_reviews (
    id                    BIGSERIAL PRIMARY KEY,
    user_id               BIGINT NOT NULL REFERENCES users (id),
    local_date            DATE NOT NULL,

    max_smoking_craving   SMALLINT,
    max_alcohol_craving   SMALLINT,

    hardest_moment        TEXT,
    best_coping_action    TEXT,

    smoked                BOOLEAN,
    drank_alcohol         BOOLEAN,

    created_at            TIMESTAMPTZ NOT NULL,
    updated_at            TIMESTAMPTZ,

    UNIQUE (user_id, local_date)
);
```

`UNIQUE (user_id, local_date)` обязателен, на нём держится upsert и дозаполнение задним числом.

### Таблица `jobs`

```sql
CREATE TABLE jobs (
    id         BIGSERIAL PRIMARY KEY,
    user_id    BIGINT NOT NULL REFERENCES users (id),
    kind       TEXT NOT NULL,       -- checkin_prompt | craving_followup | snooze
    payload    JSONB,               -- например {"checkin_id":123,"which":"smoking"}
    fire_at    TIMESTAMPTZ NOT NULL,
    status     TEXT NOT NULL DEFAULT 'pending',  -- pending | done | cancelled | expired
    created_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX idx_jobs_due ON jobs (status, fire_at);
```

`user_id` в `jobs` обязателен: поллер должен знать, в какой чат отправлять follow-up или отложенное приглашение.

## 15. Статистика

Для MVP достаточно текстовой статистики. За 7 дней:

* количество check-in;
* среднее настроение, средний стресс, среднее раздражение;
* средняя и максимальная тяга к курению;
* средняя и максимальная тяга к алкоголю;
* среднее удовольствие от жизни;
* дни без сигарет и дни без алкоголя (по `daily_reviews`);
* частые триггеры (по `context`);
* частые способы справиться (по `coping_action`);
* среднее снижение тяги после follow-up.

Пример по follow-up:

```text
Высокая тяга возникала: 8 раз
Средний уровень: 7.6

Через 20 минут:
средний уровень: 4.8

Среднее изменение: -2.8
```

## 16. UX-требования

Принцип: минимум набора текста, максимум кнопок.

* один вопрос на одно сообщение;
* кнопки для большинства ответов;
* минимум свободного текста;
* возможность отменить check-in;
* не отправлять следующий вопрос дважды;
* не запускать одновременно два check-in;
* после завершения короткий итог.

Пример итога:

```text
Готово.

Настроение: 🙂
Стресс: 6/10
Раздражение: 5/10
Курение: 7/10
Алкоголь: 2/10

Ты отметил:
нужен отдых → прогулка
```

При редактировании после изменения поля показывается обновлённое значение и остаётся возможность править дальше.

## 17. Состояние диалога

Конечный автомат состояний. Состояния держатся в in-memory карте `map[int64]*Conversation` под мьютексом, ключ это Telegram user id, поэтому диалоги разных пользователей идут параллельно и не мешают друг другу. Незавершённый check-in при рестарте теряется, это приемлемо, так как проходится за полминуты.

Ограничение "не запускать два check-in одновременно" действует в пределах одного пользователя.

Персистентность нужна не активному диалогу, а таблице `jobs`, чтобы не терялись follow-up и snooze.

Стандартный поток:

```text
IDLE
→ MOOD
→ STRESS
→ IRRITATION
→ SMOKING
→ ALCOHOL
→ LIFE_ENJOYMENT
→ NEED
→ CONTEXT
→ COPING_ACTION
→ COMPLETE
```

Переход к `NEED`, `CONTEXT`, `COPING_ACTION` только при высокой тяге. Незавершённый check-in не сохраняется как полноценная запись.

Отдельный режим для редактирования одного поля работает вне основного потока, это единичный `UPDATE` без прохождения всей цепочки.

## 18. Доступ и безопасность

Бот открыт для всех: любой Telegram-пользователь может написать боту и начать вести свои записи. Аллоу-листа нет.

Изоляция данных обеспечивается на уровне запросов:

* `user_id` берётся только из апдейта Telegram, никогда из `callback_data` или текста сообщения;
* каждый `SELECT`, `UPDATE` и агрегат содержит условие по `user_id`;
* при открытии или правке записи по её `id` дополнительно проверяется владелец; чужая запись отвечает как несуществующая;
* состояние диалога и клавиатуры привязаны к конкретному пользователю.

Не логировать Telegram Bot Token. Не коммитить `.env` и данные. В логах использовать числовой `user_id`, без имён и текстов ответов.

`.gitignore`:

```gitignore
.env
data/
/telegram-tracker
```

## 19. Структура проекта

```text
telegram-tracker/
├── cmd/
│   └── bot/
│       └── main.go
├── internal/
│   ├── config/          # env → Config, валидация
│   ├── model/           # User, Checkin, CravingFollowup, DailyReview, Job
│   ├── repository/      # pgx: интерфейс Repo + repo{pool, log}
│   ├── service/         # check-in FSM, итог дня, история, статистика
│   ├── telegram/        # транспорт: роутинг, хендлеры, клавиатуры, recover
│   ├── scheduler/       # gocron + поллер таблицы jobs
│   └── errorx/          # доменные ошибки
├── migrations/
│   ├── migrations.go    # //go:embed postgres
│   └── postgres/
│       ├── 000001_init.up.sql
│       └── 000001_init.down.sql
├── data/
│   └── backups/         # pg_dump, bind-mount, не в образе
├── Dockerfile
├── docker-compose.yml
├── .dockerignore
├── go.mod
├── go.sum
├── .env.example
├── .gitignore
├── Makefile
└── README.md
```

`internal/` использован намеренно, это приложение, а не библиотека.

Слои не смешиваются:

* `telegram/` — только транспорт (апдейты, клавиатуры, текст сообщений);
* `service/` — бизнес-логика и оркестрация;
* `repository/` — SQL и маппинг строк; `user_id` фильтруется здесь;
* `model/` не протекает в хендлеры: хендлер получает типы/результаты сервиса.

## 20. Логирование

Стандартный `log/slog`, текстовый или JSON handler.

Логировать: запуск и остановку, ошибки Telegram API, начало и завершение check-in, срабатывание job, итог рассылки (сколько получателей, сколько ошибок), ошибки БД, применение миграций golang-migrate (версия схемы), выполнение бэкапа.

Записи, относящиеся к пользователю, снабжать полем `user_id`.

Не логировать чувствительные ответы пользователя без необходимости. Не логировать `TELEGRAM_BOT_TOKEN`, `POSTGRES_PASSWORD` и `DATABASE_URL`.

В контейнере логи только в stdout/stderr, без файлов. Так их забирает `docker compose logs`.

## 21. Обработка ошибок

Бот не должен падать при:

* повторном нажатии inline-кнопки;
* устаревшем callback;
* временной ошибке Telegram API;
* ошибке записи в Postgres;
* рестарте контейнера;
* сообщении вместо ожидаемого callback;
* блокировке бота одним из пользователей во время рассылки.

При неожиданной ошибке в диалоге:

```text
Что-то пошло не так. Check-in остановлен. Можно начать новый через /checkin.
```

Ошибка попадает в лог. На верхнем уровне обработчиков стоит recover, чтобы паника в одном апдейте не роняла процесс и не задевала других пользователей.

Контейнер останавливается через SIGTERM. Процесс должен его обработать: остановить long polling и планировщик, закрыть пул Postgres, затем выйти. Без этого `docker compose stop` режет процесс SIGKILL'ом после `stop_grace_period`.

## 22. Развёртывание на VPS

Единственный способ — Docker Compose. Сервисы: `postgres`, `bot`, `backup`.

### 22.1. Образ бота

* статичная сборка `CGO_ENABLED=0`, финальный stage без компилятора;
* в образе есть CA-сертификаты (исходящий HTTPS к Telegram API);
* часовой пояс `TIMEZONE` резолвится внутри контейнера: пакет `tzdata` и `import _ "time/tzdata"` в `main`;
* процесс слушает SIGTERM/SIGINT и закрывает пул Postgres;
* SQL-миграции golang-migrate копируются в бинарь через `embed.FS`;
* контейнер бота не публикует порты и не монтирует данные: он stateless, состояние в Postgres;
* процесс не root;
* `.env` и `data/` не копируются в образ (`.dockerignore`).

Multi-stage `Dockerfile`:

```dockerfile
FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/telegram-tracker ./cmd/bot

FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata \
    && adduser -D -u 1000 bot
WORKDIR /app
COPY --from=build /out/telegram-tracker /app/telegram-tracker
USER bot
ENTRYPOINT ["/app/telegram-tracker"]
```

`.dockerignore`:

```text
.git
data
.env
docs
```

`import _ "time/tzdata"` в `cmd/bot/main.go` обязателен: alpine с `tzdata` покрывает `TIMEZONE=Europe/Belgrade`, встроенный zoneinfo не даст упасть `time.LoadLocation`.

### 22.2. Docker Compose

`docker-compose.yml`:

```yaml
services:
  postgres:
    image: postgres:16-alpine
    container_name: telegram-tracker-db
    restart: unless-stopped
    env_file: .env
    environment:
      POSTGRES_USER: ${POSTGRES_USER}
      POSTGRES_PASSWORD: ${POSTGRES_PASSWORD}
      POSTGRES_DB: ${POSTGRES_DB}
    volumes:
      - postgres_data:/var/lib/postgresql/data
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U ${POSTGRES_USER} -d ${POSTGRES_DB}"]
      interval: 5s
      timeout: 3s
      retries: 20
    # порт 5432 наружу не публиковать

  bot:
    build: .
    container_name: telegram-tracker
    restart: unless-stopped
    stop_grace_period: 20s
    env_file: .env
    depends_on:
      postgres:
        condition: service_healthy

  backup:
    image: postgres:16-alpine
    container_name: telegram-tracker-backup
    restart: unless-stopped
    env_file: .env
    depends_on:
      postgres:
        condition: service_healthy
    volumes:
      - ./data/backups:/backups
    entrypoint: ["/bin/sh", "-c"]
    command: >
      while true; do
        ts=$$(date -u +%Y-%m-%d);
        PGPASSWORD="$$POSTGRES_PASSWORD" pg_dump
          -h postgres -U "$$POSTGRES_USER" -d "$$POSTGRES_DB"
          -Fc -f "/backups/bot-$$ts.dump";
        ls -1t /backups/bot-*.dump 2>/dev/null | tail -n +$$((BACKUP_KEEP+1)) | xargs -r rm --;
        sleep 86400;
      done

volumes:
  postgres_data:
```

На хосте заранее:

```bash
mkdir -p data/backups
cp .env.example .env
# заполнить TELEGRAM_BOT_TOKEN, POSTGRES_PASSWORD и DATABASE_URL
```

Named volume `postgres_data` обязателен. Без него кластер пропадает при пересоздании контейнера. `docker compose down` volume не трогает, пока нет `-v`.

Бот стартует только после `pg_isready`. Секреты только через `.env` / `env_file`, не через `Dockerfile` и не через захардкоженный `environment:`.

### 22.3. Запуск

```bash
docker compose up -d --build
docker compose logs -f bot
```

Обновление (пересборка бота, Postgres не пересоздаётся):

```bash
docker compose up -d --build bot
```

Остановка:

```bash
docker compose stop
```

### 22.4. Резервное копирование

Данные накопительные и личные, терять их нельзя. Раз в сутки сервис `backup` делает `pg_dump -Fc` в `BACKUP_DIR` (`./data/backups` на хосте). Хранить последние `BACKUP_KEEP` копий, более старые удалять.

Восстановление:

```bash
docker compose exec -T postgres pg_restore -U "$POSTGRES_USER" -d "$POSTGRES_DB" --clean --if-exists < data/backups/bot-2026-08-18.dump
```

## 23. Запуск

```bash
docker compose up -d --build
```

После запуска бот:

1. читает конфигурацию из окружения;
2. применяет миграции golang-migrate по `DATABASE_URL` (`up` / `to_version` / `step_back`); при ошибке или dirty выходит;
3. поднимает `pgxpool`;
4. поднимает `gocron` с суточными заданиями;
5. запускает поллер таблицы `jobs` и подхватывает незакрытые задания;
6. запускает Telegram long polling;
7. работает до SIGTERM/SIGINT, затем закрывает планировщик и пул Postgres и выходит.

## 24. MVP Definition of Done

MVP готов, если:

* бот и Postgres поднимаются одной командой `docker compose up -d`;
* есть `Dockerfile`, `docker-compose.yml` и `.dockerignore`;
* схема создаётся и обновляется golang-migrate при старте бота; есть парные `.up.sql` / `.down.sql`;
* данные Postgres живут в named volume, дампы — в `./data/backups`;
* порт 5432 наружу не опубликован;
* `TIMEZONE` резолвится внутри контейнера, расписание check-in идёт по локальному времени;
* процесс корректно завершается по SIGTERM;
* ботом может пользоваться любой Telegram-пользователь, запись заводится при первом обращении;
* данные разделены по `user_id`, пользователь не видит и не может изменить чужие записи;
* автоматические check-in рассылаются всем активным пользователям, заблокировавшие бота помечаются неактивными;
* `/checkin` запускает полный опрос, ответы сохраняются в Postgres;
* высокая тяга вызывает дополнительные вопросы;
* высокая тяга ставит follow-up через 20 минут, и follow-up переживает рестарт (таблица `jobs`);
* автоматический check-in идёт по расписанию, можно отложить или пропустить;
* `/today` показывает статистику дня, `/stats` за 7 дней;
* `/history` показывает прошлые дни, день можно открыть;
* итог дня можно дозаполнить задним числом (upsert по `user_id, local_date`);
* значения существующего check-in можно отредактировать без создания дублей;
* время хранится в UTC (`TIMESTAMPTZ`), группировка по дням через `local_date`;
* работает ежесуточный `pg_dump`;
* данные переживают рестарт контейнеров;
* токены, пароль Postgres и `DATABASE_URL` вынесены в окружение, не попадают в образ;
* есть README с инструкцией запуска через Docker Compose.

## 25. Следующий этап после MVP

* графики динамики;
* CSV / JSON экспорт;
* анализ по времени суток и поиск самого опасного времени;
* корреляция стресса и тяги;
* сравнение рабочих и выходных дней;
* анализ самых эффективных способов справляться;
* персональное расписание и часовой пояс на каждого пользователя;
* изменение расписания прямо через Telegram;
* ежедневные и еженедельные отчёты;
* журнал правок (кто и когда менял запись);
* перенос бэкапов на внешнее хранилище.
