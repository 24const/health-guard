package scheduler

import (
	"context"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"telegram-tracker/internal/config"
	"telegram-tracker/internal/model"
	"telegram-tracker/internal/service"
	"telegram-tracker/internal/telegram"

	"github.com/go-co-op/gocron/v2"
)

type Sender interface {
	SendTo(userID int64, r *service.Reply) error
}

type Scheduler struct {
	cfg  *config.Config
	svc  *service.Service
	send Sender
	log  *slog.Logger
	loc  *time.Location
	cron gocron.Scheduler
}

func New(cfg *config.Config, svc *service.Service, send Sender, loc *time.Location, log *slog.Logger) (*Scheduler, error) {
	cron, err := gocron.NewScheduler(gocron.WithLocation(loc))
	if err != nil {
		return nil, err
	}
	s := &Scheduler{cfg: cfg, svc: svc, send: send, log: log, loc: loc, cron: cron}
	if err := s.registerDaily(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Scheduler) Start() { s.cron.Start() }

func (s *Scheduler) Stop() error { return s.cron.Shutdown() }

func (s *Scheduler) registerDaily() error {
	for _, clock := range s.cfg.CheckinTimes {
		h, m, err := parseHM(clock)
		if err != nil {
			return err
		}
		at := gocron.NewAtTime(uint(h), uint(m), 0)
		times := gocron.NewAtTimes(at)
		_, err = s.cron.NewJob(gocron.DailyJob(1, times), gocron.NewTask(func() {
			s.broadcast(context.Background(), "checkin", service.CheckinPrompt())
		}))
		if err != nil {
			return err
		}
	}

	h, m, err := parseHM(s.cfg.EveningReviewTime)
	if err != nil {
		return err
	}
	at := gocron.NewAtTime(uint(h), uint(m), 0)
	times := gocron.NewAtTimes(at)
		_, err = s.cron.NewJob(gocron.DailyJob(1, times), gocron.NewTask(func() {
			s.broadcastEvening(context.Background())
		}))
	return err
}

func (s *Scheduler) broadcast(ctx context.Context, kind string, reply *service.Reply) {
	ids, err := s.svc.ListActiveUserIDs(ctx)
	if err != nil {
		s.log.Error("list active users", slog.Any("err", err))
		return
	}
	s.sendToUsers(ctx, kind, ids, reply)
}

func (s *Scheduler) broadcastEvening(ctx context.Context) {
	day := s.svc.LocalDate(time.Now())
	ids, err := s.svc.ListEveningReviewRecipients(ctx, day)
	if err != nil {
		s.log.Error("list evening review recipients", slog.Any("err", err))
		return
	}
	s.sendToUsers(ctx, "evening", ids, service.EveningPromptFor(day))
}

func (s *Scheduler) sendToUsers(ctx context.Context, kind string, ids []int64, reply *service.Reply) {
	var ok, fail int
	for _, id := range ids {
		if err := s.send.SendTo(id, reply); err != nil {
			fail++
			s.log.Error("broadcast send", slog.Int64("user_id", id), slog.String("kind", kind), slog.Any("err", err))
			if telegram.IsBlocked(err) {
				_ = s.svc.DeactivateUser(ctx, id)
			}
		} else {
			ok++
		}
		time.Sleep(80 * time.Millisecond)
	}
	s.log.Info("broadcast done", slog.String("kind", kind), slog.Int("ok", ok), slog.Int("fail", fail))
}

func (s *Scheduler) RunPoller(ctx context.Context) {
	t := time.NewTicker(s.cfg.JobPollInterval)
	defer t.Stop()
	s.pollOnce(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.pollOnce(ctx)
		}
	}
}

func (s *Scheduler) pollOnce(ctx context.Context) {
	jobs, err := s.svc.ClaimJobs(ctx)
	if err != nil {
		s.log.Error("claim jobs", slog.Any("err", err))
		return
	}
	for _, job := range jobs {
		s.log.Info("job due", slog.Int64("user_id", job.UserID), slog.Int64("job_id", job.ID), slog.String("kind", string(job.Kind)))
		reply, err := s.svc.ProcessJob(ctx, job)
		if err != nil {
			s.log.Error("process job", slog.Int64("user_id", job.UserID), slog.Any("err", err))
			continue
		}
		if err := s.send.SendTo(job.UserID, reply); err != nil {
			s.log.Error("job send", slog.Int64("user_id", job.UserID), slog.Any("err", err))
			if telegram.IsBlocked(err) {
				_ = s.svc.DeactivateUser(ctx, job.UserID)
			}
			continue
		}
		_ = s.svc.MarkJob(ctx, job.ID, model.JobDone)
	}
}

func parseHM(clock string) (int, int, error) {
	parts := strings.Split(clock, ":")
	h, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0, 0, err
	}
	m, err := strconv.Atoi(parts[1])
	if err != nil {
		return 0, 0, err
	}
	return h, m, nil
}
