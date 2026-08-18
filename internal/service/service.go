package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"telegram-tracker/internal/config"
	"telegram-tracker/internal/errorx"
	"telegram-tracker/internal/model"
	"telegram-tracker/internal/repository"
)

const highCraving int16 = 6

type Button struct {
	Text string
	Data string
}

type Reply struct {
	Text   string
	Inline [][]Button
}

type Service struct {
	repo repository.Repo
	cfg  *config.Config
	loc  *time.Location
	conv *store
	log  *slog.Logger
}

func New(repo repository.Repo, cfg *config.Config, loc *time.Location, log *slog.Logger) *Service {
	return &Service{repo: repo, cfg: cfg, loc: loc, conv: newStore(), log: log}
}

func (s *Service) EnsureUser(ctx context.Context, userID int64, username, firstName string) error {
	return s.repo.UpsertUser(ctx, userID, username, firstName)
}

func (s *Service) DeactivateUser(ctx context.Context, userID int64) error {
	s.log.Info("deactivating user after delivery failure", slog.Int64("user_id", userID))
	return s.repo.SetUserActive(ctx, userID, false)
}

func (s *Service) ListActiveUserIDs(ctx context.Context) ([]int64, error) {
	return s.repo.ListActiveUserIDs(ctx)
}

func (s *Service) ListEveningReviewRecipients(ctx context.Context, localDate time.Time) ([]int64, error) {
	return s.repo.ListActiveUserIDsWithoutReview(ctx, localDate)
}

func (s *Service) LocalDate(t time.Time) time.Time {
	lt := t.In(s.loc)
	return time.Date(lt.Year(), lt.Month(), lt.Day(), 0, 0, 0, 0, time.UTC)
}

func (s *Service) StartHelp() *Reply {
	return &Reply{Text: "Привет. Это бот для коротких check-in: настроение, стресс, тяга.\n\nКнопки внизу повторяют команды:\n📝 Check-in — прямо сейчас\n📅 Сегодня — сводка дня\n📚 История — прошлые дни\n✏️ Правка — изменить записи\n📊 Статистика — 7 дней\n❌ Отмена — остановить текущий опрос"}
}

func (s *Service) Cancel(userID int64) *Reply {
	s.conv.del(userID)
	return &Reply{Text: "Ок, остановил. Можно начать заново через /checkin."}
}

func (s *Service) StartCheckin(ctx context.Context, userID int64, source model.CheckinSource) (*Reply, error) {
	if s.conv.busy(userID) {
		return &Reply{Text: "Сначала закончи текущий опрос или нажми /cancel."}, errorx.ErrBusy
	}
	s.conv.put(userID, &Conversation{
		State:  StateMood,
		Source: source,
		Draft:  model.Checkin{UserID: userID, Source: source},
	})
	s.log.Info("checkin started", slog.Int64("user_id", userID), slog.String("source", string(source)))
	return moodPrompt(), nil
}

func (s *Service) HandleCallback(ctx context.Context, userID int64, data string) (*Reply, error) {
	parts := strings.Split(data, ":")
	if len(parts) == 0 {
		return nil, errorx.ErrInvalidInput
	}
	switch parts[0] {
	case "p":
		return s.handlePrompt(ctx, userID, parts)
	case "a":
		return s.handleScale(ctx, userID, parts)
	case "c":
		return s.handleChoice(ctx, userID, parts)
	case "o":
		return s.handleOther(userID, parts)
	case "f":
		return s.handleFollowup(ctx, userID, parts)
	case "hd":
		if len(parts) != 2 {
			return nil, errorx.ErrInvalidInput
		}
		return s.DayScreen(ctx, userID, parts[1])
	case "ci":
		if len(parts) != 2 {
			return nil, errorx.ErrInvalidInput
		}
		id, err := strconv.ParseInt(parts[1], 10, 64)
		if err != nil {
			return nil, errorx.ErrInvalidInput
		}
		return s.CheckinScreen(ctx, userID, id)
	case "ef":
		return s.startEdit(ctx, userID, parts)
	case "ev":
		return s.applyEdit(ctx, userID, parts)
	case "rs":
		if len(parts) != 2 {
			return nil, errorx.ErrInvalidInput
		}
		return s.StartReview(ctx, userID, parts[1])
	case "ra":
		return s.handleReview(ctx, userID, parts)
	case "hist":
		n := 7
		if len(parts) == 2 {
			if v, err := strconv.Atoi(parts[1]); err == nil && v > 0 && v <= 31 {
				n = v
			}
		}
		return s.History(ctx, userID, n)
	default:
		return nil, errorx.ErrStaleCallback
	}
}

func (s *Service) HandleText(ctx context.Context, userID int64, text string) (*Reply, error) {
	c := s.conv.get(userID)
	if c == nil || c.State != StateWaitText {
		return nil, errorx.ErrIdle
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return &Reply{Text: "Напиши текстом, пожалуйста."}, nil
	}
	switch c.TextField {
	case "need":
		c.Draft.Need = &text
		c.State = StateContext
		s.conv.put(userID, c)
		return choicePrompt("Что сейчас происходит?", "context", ContextOptions), nil
	case "context":
		c.Draft.Context = &text
		c.State = StateCoping
		s.conv.put(userID, c)
		return choicePrompt("Что попробуешь сделать вместо сигареты или алкоголя?", "coping", CopingOptions), nil
	case "coping":
		c.Draft.CopingAction = &text
		return s.completeCheckin(ctx, userID, c)
	case "hardest":
		c.Review.HardestMoment = &text
		c.State = StateReviewCoping
		s.conv.put(userID, c)
		return choicePrompt("Что сегодня помогло больше всего?", "rv_coping", CopingOptions), nil
	case "rv_coping":
		c.Review.BestCopingAction = &text
		c.State = StateReviewSmoked
		s.conv.put(userID, c)
		return yesNoPrompt("Курил сегодня?", "smoked"), nil
	case "edit":
		if err := s.repo.UpdateCheckinField(ctx, userID, c.EditID, c.EditField, text); err != nil {
			return nil, err
		}
		s.conv.del(userID)
		return s.CheckinScreen(ctx, userID, c.EditID)
	default:
		s.conv.del(userID)
		return nil, errorx.ErrInvalidInput
	}
}

func (s *Service) handlePrompt(ctx context.Context, userID int64, parts []string) (*Reply, error) {
	if len(parts) != 2 {
		return nil, errorx.ErrInvalidInput
	}
	switch parts[1] {
	case "start":
		s.conv.del(userID)
		return s.StartCheckin(ctx, userID, model.SourceScheduled)
	case "snooze":
		job := &model.Job{
			UserID:    userID,
			Kind:      model.JobKindSnooze,
			FireAt:    time.Now().UTC().Add(s.cfg.SnoozeDelay),
			Status:    model.JobPending,
			CreatedAt: time.Now().UTC(),
		}
		if err := s.repo.CreateJob(ctx, job); err != nil {
			return nil, err
		}
		return &Reply{Text: "Ок, напомню чуть позже."}, nil
	case "skip":
		now := time.Now().UTC()
		id, err := s.repo.CreateCheckin(ctx, &model.Checkin{
			UserID:    userID,
			CreatedAt: now,
			LocalDate: s.LocalDate(now),
			Source:    model.SourceScheduled,
			Status:    model.StatusSkipped,
		})
		if err != nil {
			return nil, err
		}
		s.log.Info("checkin skipped", slog.Int64("user_id", userID), slog.Int64("checkin_id", id))
		return &Reply{Text: "Пропустил. Если передумаешь — /checkin."}, nil
	default:
		return nil, errorx.ErrInvalidInput
	}
}

func (s *Service) handleScale(ctx context.Context, userID int64, parts []string) (*Reply, error) {
	if len(parts) != 3 {
		return nil, errorx.ErrInvalidInput
	}
	v, err := strconv.Atoi(parts[2])
	if err != nil {
		return nil, errorx.ErrInvalidInput
	}
	n := int16(v)
	c := s.conv.get(userID)
	if c == nil {
		return nil, errorx.ErrStaleCallback
	}
	switch parts[1] {
	case "mood":
		if c.State != StateMood || n < 1 || n > 5 {
			return nil, errorx.ErrStaleCallback
		}
		c.Draft.Mood = &n
		c.State = StateStress
		s.conv.put(userID, c)
		return scalePrompt("Уровень напряжения сейчас?", "stress"), nil
	case "stress":
		if c.State != StateStress || n < 0 || n > 10 {
			return nil, errorx.ErrStaleCallback
		}
		c.Draft.Stress = &n
		c.State = StateIrritation
		s.conv.put(userID, c)
		return scalePrompt("Насколько ты сейчас раздражён?", "irritation"), nil
	case "irritation":
		if c.State != StateIrritation || n < 0 || n > 10 {
			return nil, errorx.ErrStaleCallback
		}
		c.Draft.Irritation = &n
		c.State = StateSmoking
		s.conv.put(userID, c)
		return scalePrompt("Насколько сейчас хочется курить?", "smoking"), nil
	case "smoking":
		if c.State != StateSmoking || n < 0 || n > 10 {
			return nil, errorx.ErrStaleCallback
		}
		c.Draft.SmokingCraving = &n
		c.State = StateAlcohol
		s.conv.put(userID, c)
		return scalePrompt("Насколько сейчас хочется выпить?", "alcohol"), nil
	case "alcohol":
		if c.State != StateAlcohol || n < 0 || n > 10 {
			return nil, errorx.ErrStaleCallback
		}
		c.Draft.AlcoholCraving = &n
		c.State = StateLife
		s.conv.put(userID, c)
		return scalePrompt("Насколько сейчас жизнь ощущается приятной или наполненной?", "life"), nil
	case "life":
		if c.State != StateLife || n < 0 || n > 10 {
			return nil, errorx.ErrStaleCallback
		}
		c.Draft.LifeEnjoyment = &n
		if high(*c.Draft.SmokingCraving) || high(*c.Draft.AlcoholCraving) {
			c.State = StateNeed
			s.conv.put(userID, c)
			return choicePrompt("Чего тебе сейчас больше всего хочется на самом деле?", "need", NeedOptions), nil
		}
		return s.completeCheckin(ctx, userID, c)
	default:
		return nil, errorx.ErrInvalidInput
	}
}

func (s *Service) handleChoice(ctx context.Context, userID int64, parts []string) (*Reply, error) {
	if len(parts) != 3 {
		return nil, errorx.ErrInvalidInput
	}
	idx, err := strconv.Atoi(parts[2])
	if err != nil {
		return nil, errorx.ErrInvalidInput
	}
	c := s.conv.get(userID)
	if c == nil {
		return nil, errorx.ErrStaleCallback
	}
	switch parts[1] {
	case "need":
		if c.State != StateNeed {
			return nil, errorx.ErrStaleCallback
		}
		val, err := optionIndex(NeedOptions, idx)
		if err != nil {
			return nil, errorx.ErrInvalidInput
		}
		if IsOther(val) {
			return s.waitText(userID, c, "need", "Напиши своими словами."), nil
		}
		c.Draft.Need = &val
		c.State = StateContext
		s.conv.put(userID, c)
		return choicePrompt("Что сейчас происходит?", "context", ContextOptions), nil
	case "context":
		if c.State != StateContext {
			return nil, errorx.ErrStaleCallback
		}
		val, err := optionIndex(ContextOptions, idx)
		if err != nil {
			return nil, errorx.ErrInvalidInput
		}
		if IsOther(val) {
			return s.waitText(userID, c, "context", "Напиши своими словами."), nil
		}
		c.Draft.Context = &val
		c.State = StateCoping
		s.conv.put(userID, c)
		return choicePrompt("Что попробуешь сделать вместо сигареты или алкоголя?", "coping", CopingOptions), nil
	case "coping":
		if c.State != StateCoping {
			return nil, errorx.ErrStaleCallback
		}
		val, err := optionIndex(CopingOptions, idx)
		if err != nil {
			return nil, errorx.ErrInvalidInput
		}
		if IsOther(val) {
			return s.waitText(userID, c, "coping", "Напиши своими словами."), nil
		}
		c.Draft.CopingAction = &val
		return s.completeCheckin(ctx, userID, c)
	case "rv_hardest":
		if c.State != StateReviewHardest {
			return nil, errorx.ErrStaleCallback
		}
		val, err := optionIndex(HardestOptions, idx)
		if err != nil {
			return nil, errorx.ErrInvalidInput
		}
		if IsOther(val) {
			return s.waitText(userID, c, "hardest", "Напиши самый сложный момент."), nil
		}
		c.Review.HardestMoment = &val
		c.State = StateReviewCoping
		s.conv.put(userID, c)
		return choicePrompt("Что сегодня помогло больше всего?", "rv_coping", CopingOptions), nil
	case "rv_coping":
		if c.State != StateReviewCoping {
			return nil, errorx.ErrStaleCallback
		}
		val, err := optionIndex(CopingOptions, idx)
		if err != nil {
			return nil, errorx.ErrInvalidInput
		}
		if IsOther(val) {
			return s.waitText(userID, c, "rv_coping", "Напиши, что помогло."), nil
		}
		c.Review.BestCopingAction = &val
		c.State = StateReviewSmoked
		s.conv.put(userID, c)
		return yesNoPrompt("Курил сегодня?", "smoked"), nil
	default:
		return nil, errorx.ErrInvalidInput
	}
}

func (s *Service) handleOther(userID int64, parts []string) (*Reply, error) {
	if len(parts) != 2 {
		return nil, errorx.ErrInvalidInput
	}
	c := s.conv.get(userID)
	if c == nil {
		return nil, errorx.ErrStaleCallback
	}
	return s.waitText(userID, c, parts[1], "Напиши своими словами."), nil
}

func (s *Service) waitText(userID int64, c *Conversation, field, prompt string) *Reply {
	c.State = StateWaitText
	c.TextField = field
	s.conv.put(userID, c)
	return &Reply{Text: prompt}
}

func (s *Service) completeCheckin(ctx context.Context, userID int64, c *Conversation) (*Reply, error) {
	now := time.Now().UTC()
	c.Draft.UserID = userID
	c.Draft.CreatedAt = now
	c.Draft.LocalDate = s.LocalDate(now)
	c.Draft.Status = model.StatusCompleted
	id, err := s.repo.CreateCheckin(ctx, &c.Draft)
	if err != nil {
		s.conv.del(userID)
		return nil, err
	}
	c.Draft.ID = id
	s.log.Info("checkin completed", slog.Int64("user_id", userID), slog.Int64("checkin_id", id))

	if highPtr(c.Draft.SmokingCraving) || highPtr(c.Draft.AlcoholCraving) {
		which := "both"
		if highPtr(c.Draft.SmokingCraving) && !highPtr(c.Draft.AlcoholCraving) {
			which = "smoking"
		} else if highPtr(c.Draft.AlcoholCraving) && !highPtr(c.Draft.SmokingCraving) {
			which = "alcohol"
		}
		payload, _ := json.Marshal(model.FollowupPayload{CheckinID: id, Which: which})
		if err := s.repo.CreateJob(ctx, &model.Job{
			UserID:    userID,
			Kind:      model.JobKindCravingFollowup,
			Payload:   payload,
			FireAt:    now.Add(s.cfg.FollowupDelay),
			Status:    model.JobPending,
			CreatedAt: now,
		}); err != nil {
			s.log.Error("schedule followup", slog.Int64("user_id", userID), slog.Any("err", err))
		}
	}

	s.conv.del(userID)
	return &Reply{Text: formatCheckinSummary(&c.Draft)}, nil
}

func (s *Service) StartFollowup(userID int64, payload model.FollowupPayload) *Reply {
	c := &Conversation{
		Followup:      model.CravingFollowup{CheckinID: payload.CheckinID},
		FollowupWhich: payload.Which,
	}
	if payload.Which == "alcohol" {
		c.State = StateFollowupAlcohol
		s.conv.put(userID, c)
		return scalePrompt("Прошло немного времени. Насколько сейчас хочется выпить?", "fu_a")
	}
	c.State = StateFollowupSmoking
	s.conv.put(userID, c)
	return scalePrompt("Прошло немного времени. Насколько сейчас хочется курить?", "fu_s")
}

func (s *Service) handleFollowup(ctx context.Context, userID int64, parts []string) (*Reply, error) {
	if len(parts) != 3 {
		return nil, errorx.ErrInvalidInput
	}
	n, err := strconv.Atoi(parts[2])
	if err != nil || n < 0 || n > 10 {
		return nil, errorx.ErrInvalidInput
	}
	v := int16(n)
	c := s.conv.get(userID)
	if c == nil {
		return nil, errorx.ErrStaleCallback
	}
	switch parts[1] {
	case "s":
		if c.State != StateFollowupSmoking {
			return nil, errorx.ErrStaleCallback
		}
		c.Followup.SmokingCraving = &v
		if c.FollowupWhich == "both" {
			c.State = StateFollowupAlcohol
			s.conv.put(userID, c)
			return scalePrompt("А насколько сейчас хочется выпить?", "fu_a"), nil
		}
		return s.saveFollowup(ctx, userID, c)
	case "a":
		if c.State != StateFollowupAlcohol {
			return nil, errorx.ErrStaleCallback
		}
		c.Followup.AlcoholCraving = &v
		return s.saveFollowup(ctx, userID, c)
	default:
		return nil, errorx.ErrInvalidInput
	}
}

func (s *Service) saveFollowup(ctx context.Context, userID int64, c *Conversation) (*Reply, error) {
	if _, err := s.repo.GetCheckin(ctx, userID, c.Followup.CheckinID); err != nil {
		s.conv.del(userID)
		return nil, err
	}
	c.Followup.CreatedAt = time.Now().UTC()
	if err := s.repo.CreateFollowup(ctx, &c.Followup); err != nil {
		s.conv.del(userID)
		return nil, err
	}
	s.conv.del(userID)
	return &Reply{Text: "Записал. Спасибо."}, nil
}

func (s *Service) StartReview(ctx context.Context, userID int64, dateStr string) (*Reply, error) {
	day, err := time.Parse("2006-01-02", dateStr)
	if err != nil {
		day = s.LocalDate(time.Now())
	}
	if s.conv.busy(userID) {
		return &Reply{Text: "Сначала закончи текущий опрос или /cancel."}, errorx.ErrBusy
	}
	s.conv.put(userID, &Conversation{
		State:  StateReviewMaxSmoking,
		Review: model.DailyReview{UserID: userID, LocalDate: day},
	})
	return scalePrompt("Максимальная тяга к курению сегодня?", "rv_ms"), nil
}

func (s *Service) handleReview(ctx context.Context, userID int64, parts []string) (*Reply, error) {
	if len(parts) != 3 {
		return nil, errorx.ErrInvalidInput
	}
	c := s.conv.get(userID)
	if c == nil {
		return nil, errorx.ErrStaleCallback
	}
	switch parts[1] {
	case "ms", "ma":
		n, err := strconv.Atoi(parts[2])
		if err != nil || n < 0 || n > 10 {
			return nil, errorx.ErrInvalidInput
		}
		v := int16(n)
		if parts[1] == "ms" {
			if c.State != StateReviewMaxSmoking {
				return nil, errorx.ErrStaleCallback
			}
			c.Review.MaxSmokingCraving = &v
			c.State = StateReviewMaxAlcohol
			s.conv.put(userID, c)
			return scalePrompt("Максимальная тяга к алкоголю сегодня?", "rv_ma"), nil
		}
		if c.State != StateReviewMaxAlcohol {
			return nil, errorx.ErrStaleCallback
		}
		c.Review.MaxAlcoholCraving = &v
		c.State = StateReviewHardest
		s.conv.put(userID, c)
		return choicePrompt("Самый сложный момент?", "rv_hardest", HardestOptions), nil
	case "smoked", "drank":
		yes := parts[2] == "1"
		if parts[1] == "smoked" {
			if c.State != StateReviewSmoked {
				return nil, errorx.ErrStaleCallback
			}
			c.Review.Smoked = &yes
			c.State = StateReviewDrank
			s.conv.put(userID, c)
			return yesNoPrompt("Пил алкоголь сегодня?", "drank"), nil
		}
		if c.State != StateReviewDrank {
			return nil, errorx.ErrStaleCallback
		}
		c.Review.DrankAlcohol = &yes
		now := time.Now().UTC()
		c.Review.CreatedAt = now
		c.Review.UpdatedAt = &now
		if err := s.repo.UpsertDailyReview(ctx, &c.Review); err != nil {
			s.conv.del(userID)
			return nil, err
		}
		s.conv.del(userID)
		return &Reply{Text: "Итог дня сохранён."}, nil
	default:
		return nil, errorx.ErrInvalidInput
	}
}

func (s *Service) Today(ctx context.Context, userID int64) (*Reply, error) {
	st, err := s.repo.TodayStats(ctx, userID, s.LocalDate(time.Now()))
	if err != nil {
		return nil, err
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Сегодня:\n\nCheck-in: %d\n", st.CheckinCount)
	if st.AvgSmoking != nil {
		fmt.Fprintf(&b, "\nСредняя тяга к курению: %.1f\nМаксимальная: %d\n", *st.AvgSmoking, *st.MaxSmoking)
	}
	if st.AvgAlcohol != nil {
		fmt.Fprintf(&b, "\nСредняя тяга к алкоголю: %.1f\nМаксимальная: %d\n", *st.AvgAlcohol, *st.MaxAlcohol)
	}
	if st.AvgStress != nil {
		fmt.Fprintf(&b, "\nСредний стресс: %.1f\n", *st.AvgStress)
	}
	if st.AvgIrritation != nil {
		fmt.Fprintf(&b, "Среднее раздражение: %.1f\n", *st.AvgIrritation)
	}
	if st.CheckinCount == 0 {
		b.WriteString("\nПока нет завершённых check-in.")
	}
	return &Reply{Text: b.String()}, nil
}

func (s *Service) History(ctx context.Context, userID int64, days int) (*Reply, error) {
	if days <= 0 {
		days = 7
	}
	to := s.LocalDate(time.Now())
	from := to.AddDate(0, 0, -(days - 1))
	items, err := s.repo.HistoryDays(ctx, userID, from, to)
	if err != nil {
		return nil, err
	}
	var rows [][]Button
	var b strings.Builder
	b.WriteString("История:\n")
	weekdays := []string{"Вс", "Пн", "Вт", "Ср", "Чт", "Пт", "Сб"}
	for _, d := range items {
		locDay := d.Date
		wd := weekdays[locDay.Weekday()]
		review := "итог: не заполнен"
		if d.HasReview {
			review = "итог: заполнен"
			if d.Smoked != nil && !*d.Smoked {
				review += "   🚬 нет"
			}
			if d.DrankAlcohol != nil && !*d.DrankAlcohol {
				review += "   🍷 нет"
			}
		}
		line := fmt.Sprintf("%s %s   check-in: %d   %s", wd, locDay.Format("02.01"), d.CheckinCount, review)
		b.WriteString("\n")
		b.WriteString(line)
		rows = append(rows, []Button{{Text: locDay.Format("02.01"), Data: "hd:" + locDay.Format("2006-01-02")}})
	}
	return &Reply{Text: b.String(), Inline: rows}, nil
}

func (s *Service) DayScreen(ctx context.Context, userID int64, dateStr string) (*Reply, error) {
	day, err := time.Parse("2006-01-02", dateStr)
	if err != nil {
		return nil, errorx.ErrInvalidInput
	}
	list, err := s.repo.ListCheckinsByDay(ctx, userID, day)
	if err != nil {
		return nil, err
	}
	_, revErr := s.repo.GetDailyReview(ctx, userID, day)
	hasReview := revErr == nil
	if revErr != nil && !errors.Is(revErr, errorx.ErrNotFound) {
		return nil, revErr
	}

	var b strings.Builder
	fmt.Fprintf(&b, "%s\n\nCheck-in: %d\nИтог дня: ", day.Format("02.01.2006"), len(list))
	if hasReview {
		b.WriteString("заполнен")
	} else {
		b.WriteString("не заполнен")
	}

	rows := [][]Button{{{Text: "Заполнить / изменить итог дня", Data: "rs:" + dateStr}}}
	for _, c := range list {
		label := fmt.Sprintf("%s · %s", c.CreatedAt.In(s.loc).Format("15:04"), c.Status)
		rows = append(rows, []Button{{Text: label, Data: fmt.Sprintf("ci:%d", c.ID)}})
	}
	return &Reply{Text: b.String(), Inline: rows}, nil
}

func (s *Service) CheckinScreen(ctx context.Context, userID, id int64) (*Reply, error) {
	c, err := s.repo.GetCheckin(ctx, userID, id)
	if err != nil {
		return nil, err
	}
	text := formatCheckinDetail(c)
	rows := [][]Button{
		{{Text: "Настроение", Data: fmt.Sprintf("ef:mood:%d", id)}, {Text: "Стресс", Data: fmt.Sprintf("ef:stress:%d", id)}},
		{{Text: "Раздражение", Data: fmt.Sprintf("ef:irritation:%d", id)}},
		{{Text: "Курение", Data: fmt.Sprintf("ef:smoking_craving:%d", id)}, {Text: "Алкоголь", Data: fmt.Sprintf("ef:alcohol_craving:%d", id)}},
		{{Text: "Удовольствие", Data: fmt.Sprintf("ef:life_enjoyment:%d", id)}},
		{{Text: "Потребность", Data: fmt.Sprintf("ef:need:%d", id)}, {Text: "Контекст", Data: fmt.Sprintf("ef:context:%d", id)}},
		{{Text: "Способ", Data: fmt.Sprintf("ef:coping_action:%d", id)}},
	}
	return &Reply{Text: text, Inline: rows}, nil
}

func (s *Service) startEdit(ctx context.Context, userID int64, parts []string) (*Reply, error) {
	if len(parts) != 3 {
		return nil, errorx.ErrInvalidInput
	}
	id, err := strconv.ParseInt(parts[2], 10, 64)
	if err != nil {
		return nil, errorx.ErrInvalidInput
	}
	if _, err := s.repo.GetCheckin(ctx, userID, id); err != nil {
		return nil, err
	}
	field := parts[1]
	switch field {
	case "mood":
		return moodPromptEdit(id), nil
	case "stress", "irritation", "smoking_craving", "alcohol_craving", "life_enjoyment":
		return scalePromptEdit(field, id), nil
	case "need":
		return choicePromptEdit("Потребность", field, id, NeedOptions), nil
	case "context":
		return choicePromptEdit("Контекст", field, id, ContextOptions), nil
	case "coping_action":
		return choicePromptEdit("Способ справиться", field, id, CopingOptions), nil
	default:
		return nil, errorx.ErrInvalidInput
	}
}

func (s *Service) applyEdit(ctx context.Context, userID int64, parts []string) (*Reply, error) {
	if len(parts) != 4 {
		return nil, errorx.ErrInvalidInput
	}
	id, err := strconv.ParseInt(parts[2], 10, 64)
	if err != nil {
		return nil, errorx.ErrInvalidInput
	}
	field := parts[1]
	raw := parts[3]
	var value any
	switch field {
	case "mood", "stress", "irritation", "smoking_craving", "alcohol_craving", "life_enjoyment":
		n, err := strconv.Atoi(raw)
		if err != nil {
			return nil, errorx.ErrInvalidInput
		}
		value = int16(n)
	case "need", "context", "coping_action":
		idx, err := strconv.Atoi(raw)
		if err != nil {
			return nil, errorx.ErrInvalidInput
		}
		opts := NeedOptions
		if field == "context" {
			opts = ContextOptions
		}
		if field == "coping_action" {
			opts = CopingOptions
		}
		val, err := optionIndex(opts, idx)
		if err != nil {
			return nil, errorx.ErrInvalidInput
		}
		if IsOther(val) {
			s.conv.put(userID, &Conversation{State: StateWaitText, TextField: "edit", EditID: id, EditField: field})
			return &Reply{Text: "Напиши новое значение."}, nil
		}
		value = val
	default:
		return nil, errorx.ErrInvalidInput
	}
	if err := s.repo.UpdateCheckinField(ctx, userID, id, field, value); err != nil {
		return nil, err
	}
	return s.CheckinScreen(ctx, userID, id)
}

func (s *Service) Stats(ctx context.Context, userID int64) (*Reply, error) {
	to := s.LocalDate(time.Now())
	from := to.AddDate(0, 0, -6)
	st, err := s.repo.WeekStats(ctx, userID, from, to)
	if err != nil {
		return nil, err
	}
	var b strings.Builder
	b.WriteString("7 дней:\n")
	fmt.Fprintf(&b, "\nCheck-in: %d\n", st.CheckinCount)
	if st.AvgMood != nil {
		fmt.Fprintf(&b, "Среднее настроение: %.1f\n", *st.AvgMood)
	}
	if st.AvgStress != nil {
		fmt.Fprintf(&b, "Средний стресс: %.1f\n", *st.AvgStress)
	}
	if st.AvgIrritation != nil {
		fmt.Fprintf(&b, "Среднее раздражение: %.1f\n", *st.AvgIrritation)
	}
	if st.AvgSmoking != nil {
		fmt.Fprintf(&b, "\nТяга к курению: среднее %.1f, макс %d\n", *st.AvgSmoking, *st.MaxSmoking)
	}
	if st.AvgAlcohol != nil {
		fmt.Fprintf(&b, "Тяга к алкоголю: среднее %.1f, макс %d\n", *st.AvgAlcohol, *st.MaxAlcohol)
	}
	if st.AvgLife != nil {
		fmt.Fprintf(&b, "Удовольствие от жизни: %.1f\n", *st.AvgLife)
	}
	fmt.Fprintf(&b, "\nДней без сигарет: %d из %d итогов\n", st.SmokeFreeDays, st.ReviewDays)
	fmt.Fprintf(&b, "Дней без алкоголя: %d из %d итогов\n", st.AlcoholFreeDays, st.ReviewDays)
	if len(st.TopContexts) > 0 {
		fmt.Fprintf(&b, "\nЧастые триггеры: %s\n", strings.Join(st.TopContexts, ", "))
	}
	if len(st.TopCoping) > 0 {
		fmt.Fprintf(&b, "Частые способы: %s\n", strings.Join(st.TopCoping, ", "))
	}
	if st.HighCravingCount > 0 && st.HighCravingAvg != nil && st.FollowupAvg != nil {
		fmt.Fprintf(&b, "\nВысокая тяга возникала: %d раз\nСредний уровень: %.1f\n\nЧерез %d минут:\nсредний уровень: %.1f\n\nСреднее изменение: %+.1f\n",
			st.HighCravingCount, *st.HighCravingAvg, int(s.cfg.FollowupDelay.Minutes()), *st.FollowupAvg, *st.FollowupDelta)
	}
	return &Reply{Text: b.String()}, nil
}

func (s *Service) ProcessJob(ctx context.Context, job model.Job) (*Reply, error) {
	switch job.Kind {
	case model.JobKindSnooze, model.JobKindCheckinPrompt:
		return CheckinPrompt(), nil
	case model.JobKindCravingFollowup:
		var p model.FollowupPayload
		if err := json.Unmarshal(job.Payload, &p); err != nil {
			return nil, err
		}
		return s.StartFollowup(job.UserID, p), nil
	default:
		return nil, fmt.Errorf("unknown job kind %s", job.Kind)
	}
}

func (s *Service) ClaimJobs(ctx context.Context) ([]model.Job, error) {
	return s.repo.ClaimDueJobs(ctx, time.Now().UTC(), s.cfg.JobGrace)
}

func (s *Service) MarkJob(ctx context.Context, id int64, status model.JobStatus) error {
	return s.repo.MarkJobStatus(ctx, id, status)
}

func CheckinPrompt() *Reply {
	return &Reply{
		Text: "Время короткого check-in. Как ты?",
		Inline: [][]Button{
			{{Text: "Начать", Data: "p:start"}},
			{{Text: "Через 15 минут", Data: "p:snooze"}},
			{{Text: "Пропустить", Data: "p:skip"}},
		},
	}
}

func EveningPrompt() *Reply {
	today := time.Now()
	return &Reply{
		Text: "Короткий итог дня?",
		Inline: [][]Button{
			{{Text: "Заполнить", Data: "rs:" + today.Format("2006-01-02")}},
		},
	}
}

func EveningPromptFor(t time.Time) *Reply {
	return &Reply{
		Text: "Короткий итог дня?",
		Inline: [][]Button{
			{{Text: "Заполнить", Data: "rs:" + t.Format("2006-01-02")}},
		},
	}
}

func high(v int16) bool { return v >= highCraving }

func highPtr(v *int16) bool { return v != nil && *v >= highCraving }

func moodPrompt() *Reply {
	var row []Button
	for i, label := range MoodLabels {
		row = append(row, Button{Text: label, Data: fmt.Sprintf("a:mood:%d", i+1)})
	}
	rows := chunk(row, 1)
	return &Reply{Text: "Как ты себя сейчас чувствуешь?", Inline: rows}
}

func moodPromptEdit(id int64) *Reply {
	var rows [][]Button
	for i, label := range MoodLabels {
		rows = append(rows, []Button{{Text: label, Data: fmt.Sprintf("ev:mood:%d:%d", id, i+1)}})
	}
	return &Reply{Text: "Настроение:", Inline: rows}
}

func scalePrompt(text, key string) *Reply {
	dataKey := key
	switch key {
	case "fu_s":
		dataKey = "f:s"
		return &Reply{Text: text, Inline: scaleRowsPrefixed(dataKey)}
	case "fu_a":
		dataKey = "f:a"
		return &Reply{Text: text, Inline: scaleRowsPrefixed(dataKey)}
	case "rv_ms":
		return &Reply{Text: text, Inline: scaleRowsPrefixed("ra:ms")}
	case "rv_ma":
		return &Reply{Text: text, Inline: scaleRowsPrefixed("ra:ma")}
	default:
		return &Reply{Text: text, Inline: scaleRowsPrefixed("a:" + key)}
	}
}

func scalePromptEdit(field string, id int64) *Reply {
	var rows [][]Button
	var row []Button
	max := 10
	start := 0
	if field == "mood" {
		start = 1
		max = 5
	}
	for i := start; i <= max; i++ {
		row = append(row, Button{Text: strconv.Itoa(i), Data: fmt.Sprintf("ev:%s:%d:%d", field, id, i)})
		if len(row) == 6 {
			rows = append(rows, row)
			row = nil
		}
	}
	if len(row) > 0 {
		rows = append(rows, row)
	}
	return &Reply{Text: "Новое значение:", Inline: rows}
}

func scaleRowsPrefixed(prefix string) [][]Button {
	var rows [][]Button
	var row []Button
	for i := 0; i <= 10; i++ {
		row = append(row, Button{Text: strconv.Itoa(i), Data: fmt.Sprintf("%s:%d", prefix, i)})
		if len(row) == 6 {
			rows = append(rows, row)
			row = nil
		}
	}
	if len(row) > 0 {
		rows = append(rows, row)
	}
	return rows
}

func choicePrompt(text, key string, opts []string) *Reply {
	var rows [][]Button
	for i, o := range opts {
		rows = append(rows, []Button{{Text: o, Data: fmt.Sprintf("c:%s:%d", key, i)}})
	}
	return &Reply{Text: text, Inline: rows}
}

func choicePromptEdit(text, field string, id int64, opts []string) *Reply {
	var rows [][]Button
	for i, o := range opts {
		rows = append(rows, []Button{{Text: o, Data: fmt.Sprintf("ev:%s:%d:%d", field, id, i)}})
	}
	return &Reply{Text: text, Inline: rows}
}

func yesNoPrompt(text, key string) *Reply {
	return &Reply{
		Text: text,
		Inline: [][]Button{{
			{Text: "Нет", Data: "ra:" + key + ":0"},
			{Text: "Да", Data: "ra:" + key + ":1"},
		}},
	}
}

func chunk(in []Button, n int) [][]Button {
	var out [][]Button
	for i := 0; i < len(in); i += n {
		j := i + n
		if j > len(in) {
			j = len(in)
		}
		out = append(out, in[i:j])
	}
	return out
}

func formatCheckinSummary(c *model.Checkin) string {
	var b strings.Builder
	b.WriteString("Готово.\n")
	if c.Mood != nil {
		fmt.Fprintf(&b, "\nНастроение: %s", MoodLabel(*c.Mood))
	}
	if c.Stress != nil {
		fmt.Fprintf(&b, "\nСтресс: %d/10", *c.Stress)
	}
	if c.Irritation != nil {
		fmt.Fprintf(&b, "\nРаздражение: %d/10", *c.Irritation)
	}
	if c.SmokingCraving != nil {
		fmt.Fprintf(&b, "\nКурение: %d/10", *c.SmokingCraving)
	}
	if c.AlcoholCraving != nil {
		fmt.Fprintf(&b, "\nАлкоголь: %d/10", *c.AlcoholCraving)
	}
	if c.Need != nil || c.CopingAction != nil {
		b.WriteString("\n\nТы отметил:")
		if c.Need != nil && c.CopingAction != nil {
			fmt.Fprintf(&b, "\n%s → %s", *c.Need, *c.CopingAction)
		} else if c.Need != nil {
			fmt.Fprintf(&b, "\n%s", *c.Need)
		}
	}
	return b.String()
}

func formatCheckinDetail(c *model.Checkin) string {
	var b strings.Builder
	b.WriteString("Check-in\n")
	if c.Mood != nil {
		fmt.Fprintf(&b, "\nНастроение: %s", MoodLabel(*c.Mood))
	}
	writePtr16(&b, "Стресс", c.Stress)
	writePtr16(&b, "Раздражение", c.Irritation)
	writePtr16(&b, "Курение", c.SmokingCraving)
	writePtr16(&b, "Алкоголь", c.AlcoholCraving)
	writePtr16(&b, "Удовольствие", c.LifeEnjoyment)
	writePtrStr(&b, "Потребность", c.Need)
	writePtrStr(&b, "Контекст", c.Context)
	writePtrStr(&b, "Способ", c.CopingAction)
	fmt.Fprintf(&b, "\n\nИсточник: %s · статус: %s", c.Source, c.Status)
	return b.String()
}

func writePtr16(b *strings.Builder, label string, v *int16) {
	if v != nil {
		fmt.Fprintf(b, "\n%s: %d/10", label, *v)
	}
}

func writePtrStr(b *strings.Builder, label string, v *string) {
	if v != nil && *v != "" {
		fmt.Fprintf(b, "\n%s: %s", label, *v)
	}
}
