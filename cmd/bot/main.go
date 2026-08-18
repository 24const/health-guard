package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	_ "time/tzdata"

	"telegram-tracker/internal/config"
	"telegram-tracker/internal/repository"
	"telegram-tracker/internal/scheduler"
	"telegram-tracker/internal/service"
	"telegram-tracker/internal/telegram"
	"telegram-tracker/migrations"
)

func main() {
	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	cfg, err := config.Load()
	if err != nil {
		log.Error("config", slog.Any("err", err))
		os.Exit(1)
	}

	loc, err := time.LoadLocation(cfg.Timezone)
	if err != nil {
		log.Error("timezone", slog.Any("err", err))
		os.Exit(1)
	}

	version, err := migrations.PostgresMigrate(cfg.DatabaseURL, migrations.MigrateConfig{
		Direction: migrations.Direction(cfg.MigrationDirection),
		Version:   int(cfg.MigrationVersion),
	}, migrations.GetPostgresMigrations())
	if err != nil {
		log.Error("migrations failed", slog.Any("err", err))
		os.Exit(1)
	}
	log.Info("migrations at version", slog.Int("v", version))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	pool, err := repository.OpenPool(ctx, cfg.DatabaseURL, cfg.DBMaxOpenConns, cfg.DBMinConns, cfg.DBMaxConnLifetime, cfg.DBMaxConnIdleTime)
	if err != nil {
		log.Error("database", slog.Any("err", err))
		os.Exit(1)
	}
	defer pool.Close()

	repo := repository.New(pool, log)
	svc := service.New(repo, cfg, loc, log)

	bot, err := telegram.New(cfg.TelegramBotToken, svc, log)
	if err != nil {
		log.Error("telegram", slog.Any("err", err))
		os.Exit(1)
	}

	sched, err := scheduler.New(cfg, svc, bot, loc, log)
	if err != nil {
		log.Error("scheduler", slog.Any("err", err))
		os.Exit(1)
	}
	sched.Start()
	go sched.RunPoller(ctx)

	go func() {
		log.Info("bot started")
		bot.Start()
	}()

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	<-sig
	log.Info("shutting down")
	cancel()
	bot.Stop()
	if err := sched.Stop(); err != nil {
		log.Error("scheduler stop", slog.Any("err", err))
	}
}
