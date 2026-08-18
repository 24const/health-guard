package telegram

import (
	"context"
	"errors"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"telegram-tracker/internal/errorx"
	"telegram-tracker/internal/model"
	"telegram-tracker/internal/service"

	tele "gopkg.in/telebot.v3"
)

type Bot struct {
	api *tele.Bot
	svc *service.Service
	log *slog.Logger
}

func New(token string, svc *service.Service, log *slog.Logger) (*Bot, error) {
	api, err := tele.NewBot(tele.Settings{
		Token:  token,
		Poller: &tele.LongPoller{Timeout: 10 * time.Second},
	})
	if err != nil {
		return nil, err
	}
	b := &Bot{api: api, svc: svc, log: log}
	b.routes()
	return b, nil
}

func (b *Bot) Start() { b.api.Start() }

func (b *Bot) Stop() { b.api.Stop() }

func (b *Bot) routes() {
	b.api.Use(b.recoverMw)
	b.api.Use(b.ensureUser)

	menu := &tele.ReplyMarkup{ResizeKeyboard: true}
	btnCheckin := menu.Text("📝 Check-in")
	menu.Reply(menu.Row(btnCheckin))

	b.api.Handle("/start", func(c tele.Context) error {
		return b.sendReply(c, b.svc.StartHelp(), menu)
	})
	b.api.Handle("/checkin", func(c tele.Context) error {
		r, err := b.svc.StartCheckin(c.Get("ctx").(context.Context), c.Sender().ID, model.SourceManual)
		return b.replyOrErr(c, r, err)
	})
	b.api.Handle(&btnCheckin, func(c tele.Context) error {
		r, err := b.svc.StartCheckin(c.Get("ctx").(context.Context), c.Sender().ID, model.SourceManual)
		return b.replyOrErr(c, r, err)
	})
	b.api.Handle("/cancel", func(c tele.Context) error {
		return b.sendReply(c, b.svc.Cancel(c.Sender().ID), nil)
	})
	b.api.Handle("/today", func(c tele.Context) error {
		r, err := b.svc.Today(c.Get("ctx").(context.Context), c.Sender().ID)
		return b.replyOrErr(c, r, err)
	})
	b.api.Handle("/history", func(c tele.Context) error {
		n := 7
		if args := strings.TrimSpace(c.Message().Payload); args != "" {
			if v, err := strconv.Atoi(args); err == nil {
				n = v
			}
		}
		r, err := b.svc.History(c.Get("ctx").(context.Context), c.Sender().ID, n)
		return b.replyOrErr(c, r, err)
	})
	b.api.Handle("/edit", func(c tele.Context) error {
		r, err := b.svc.History(c.Get("ctx").(context.Context), c.Sender().ID, 7)
		return b.replyOrErr(c, r, err)
	})
	b.api.Handle("/stats", func(c tele.Context) error {
		r, err := b.svc.Stats(c.Get("ctx").(context.Context), c.Sender().ID)
		return b.replyOrErr(c, r, err)
	})
	b.api.Handle(tele.OnCallback, b.onCallback)
	b.api.Handle(tele.OnText, b.onText)
}

func (b *Bot) recoverMw(next tele.HandlerFunc) tele.HandlerFunc {
	return func(c tele.Context) error {
		defer func() {
			if rec := recover(); rec != nil {
				b.log.Error("panic in handler", slog.Int64("user_id", userID(c)), slog.Any("panic", rec))
				_ = c.Send("Что-то пошло не так. Check-in остановлен. Можно начать новый через /checkin.")
				b.svc.Cancel(userID(c))
			}
		}()
		return next(c)
	}
}

func (b *Bot) ensureUser(next tele.HandlerFunc) tele.HandlerFunc {
	return func(c tele.Context) error {
		ctx := context.Background()
		c.Set("ctx", ctx)
		u := c.Sender()
		if u == nil {
			return next(c)
		}
		if err := b.svc.EnsureUser(ctx, u.ID, u.Username, u.FirstName); err != nil {
			b.log.Error("upsert user", slog.Int64("user_id", u.ID), slog.Any("err", err))
			return c.Send("Не получилось сохранить профиль. Попробуй ещё раз.")
		}
		return next(c)
	}
}

func (b *Bot) onCallback(c tele.Context) error {
	_ = c.Respond()
	data := c.Callback().Data
	data = strings.TrimPrefix(data, "\f")
	r, err := b.svc.HandleCallback(c.Get("ctx").(context.Context), c.Sender().ID, data)
	if err != nil {
		if errors.Is(err, errorx.ErrStaleCallback) || errors.Is(err, errorx.ErrBusy) || errors.Is(err, errorx.ErrNotFound) {
			if r != nil {
				return b.sendReply(c, r, nil)
			}
			return c.Send("Это уже неактуально. Начни с /checkin или /history.")
		}
		b.log.Error("callback", slog.Int64("user_id", c.Sender().ID), slog.Any("err", err))
		b.svc.Cancel(c.Sender().ID)
		return c.Send("Что-то пошло не так. Check-in остановлен. Можно начать новый через /checkin.")
	}
	return b.sendReply(c, r, nil)
}

func (b *Bot) onText(c tele.Context) error {
	if c.Message() != nil && strings.HasPrefix(c.Text(), "/") {
		return nil
	}
	r, err := b.svc.HandleText(c.Get("ctx").(context.Context), c.Sender().ID, c.Text())
	if errors.Is(err, errorx.ErrIdle) {
		return nil
	}
	return b.replyOrErr(c, r, err)
}

func (b *Bot) replyOrErr(c tele.Context, r *service.Reply, err error) error {
	if err != nil && !errors.Is(err, errorx.ErrBusy) {
		b.log.Error("handler", slog.Int64("user_id", userID(c)), slog.Any("err", err))
		b.svc.Cancel(userID(c))
		return c.Send("Что-то пошло не так. Check-in остановлен. Можно начать новый через /checkin.")
	}
	if r == nil {
		return nil
	}
	return b.sendReply(c, r, nil)
}

func (b *Bot) sendReply(c tele.Context, r *service.Reply, menu *tele.ReplyMarkup) error {
	if r == nil {
		return nil
	}
	opts := make([]any, 0, 2)
	if menu != nil {
		opts = append(opts, menu)
	}
	if markup := inlineMarkup(r.Inline); markup != nil {
		opts = append(opts, markup)
	}
	return c.Send(r.Text, opts...)
}

func inlineMarkup(rows [][]service.Button) *tele.ReplyMarkup {
	if len(rows) == 0 {
		return nil
	}
	m := &tele.ReplyMarkup{}
	var out [][]tele.InlineButton
	for _, row := range rows {
		var r []tele.InlineButton
		for _, btn := range row {
			r = append(r, tele.InlineButton{Text: btn.Text, Data: btn.Data})
		}
		out = append(out, r)
	}
	m.Inline(inlineRows(out)...)
	return m
}

func inlineRows(rows [][]tele.InlineButton) []tele.Row {
	out := make([]tele.Row, 0, len(rows))
	for _, r := range rows {
		out = append(out, r)
	}
	return out
}

func (b *Bot) SendTo(userID int64, r *service.Reply) error {
	if r == nil {
		return nil
	}
	chat := &tele.User{ID: userID}
	opts := []any{}
	if markup := inlineMarkup(r.Inline); markup != nil {
		opts = append(opts, markup)
	}
	_, err := b.api.Send(chat, r.Text, opts...)
	return err
}

func IsBlocked(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "blocked") ||
		strings.Contains(msg, "user is deactivated") ||
		strings.Contains(msg, "chat not found") ||
		strings.Contains(msg, "forbidden")
}

func userID(c tele.Context) int64 {
	if c.Sender() == nil {
		return 0
	}
	return c.Sender().ID
}
