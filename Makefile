.PHONY: run up down logs build

run:
	go run ./cmd/bot

up:
	mkdir -p data/postgres
	docker compose up -d --build

down:
	docker compose stop

logs:
	docker compose logs -f bot

build:
	CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o telegram-tracker ./cmd/bot

migrate-create:
	@test -n "$(name)" || (echo "usage: make migrate-create name=add_foo"; exit 1)
	n=$$(ls migrations/postgres/*.up.sql 2>/dev/null | wc -l | tr -d ' '); \
	n=$$(printf "%06d" $$((n+1))); \
	touch migrations/postgres/$${n}_$(name).up.sql migrations/postgres/$${n}_$(name).down.sql; \
	echo "created migrations/postgres/$${n}_$(name).{up,down}.sql"
