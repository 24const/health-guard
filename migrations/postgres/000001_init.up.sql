-- ========= UP =========

CREATE TABLE IF NOT EXISTS users (
    id          BIGINT PRIMARY KEY,
    username    TEXT,
    first_name  TEXT,
    is_active   BOOLEAN NOT NULL DEFAULT TRUE,
    created_at  TIMESTAMPTZ NOT NULL,
    updated_at  TIMESTAMPTZ
);

CREATE TABLE IF NOT EXISTS checkins (
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
    source          TEXT NOT NULL,
    status          TEXT NOT NULL DEFAULT 'completed'
);

CREATE INDEX IF NOT EXISTS idx_checkins_day ON checkins (user_id, local_date);

CREATE TABLE IF NOT EXISTS craving_followups (
    id              BIGSERIAL PRIMARY KEY,
    checkin_id      BIGINT NOT NULL REFERENCES checkins (id),
    created_at      TIMESTAMPTZ NOT NULL,
    updated_at      TIMESTAMPTZ,
    smoking_craving SMALLINT,
    alcohol_craving SMALLINT
);

CREATE TABLE IF NOT EXISTS daily_reviews (
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

CREATE TABLE IF NOT EXISTS jobs (
    id         BIGSERIAL PRIMARY KEY,
    user_id    BIGINT NOT NULL REFERENCES users (id),
    kind       TEXT NOT NULL,
    payload    JSONB,
    fire_at    TIMESTAMPTZ NOT NULL,
    status     TEXT NOT NULL DEFAULT 'pending',
    created_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_jobs_due ON jobs (status, fire_at);
