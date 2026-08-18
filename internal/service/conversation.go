package service

import (
	"fmt"
	"sync"

	"telegram-tracker/internal/model"
)

type State string

const (
	StateIdle              State = "idle"
	StateMood              State = "mood"
	StateStress            State = "stress"
	StateIrritation        State = "irritation"
	StateSmoking           State = "smoking"
	StateAlcohol           State = "alcohol"
	StateLife              State = "life"
	StateNeed              State = "need"
	StateContext           State = "context"
	StateCoping            State = "coping"
	StateWaitText          State = "wait_text"
	StateFollowupSmoking   State = "fu_smoking"
	StateFollowupAlcohol   State = "fu_alcohol"
	StateReviewMaxSmoking  State = "rv_max_smoking"
	StateReviewMaxAlcohol  State = "rv_max_alcohol"
	StateReviewHardest     State = "rv_hardest"
	StateReviewCoping      State = "rv_coping"
	StateReviewSmoked      State = "rv_smoked"
	StateReviewDrank       State = "rv_drank"
	StateEditWait          State = "edit_wait"
)

type Conversation struct {
	State        State
	Source       model.CheckinSource
	Draft        model.Checkin
	Review       model.DailyReview
	Followup     model.CravingFollowup
	FollowupWhich string
	TextField    string
	EditID       int64
	EditField    string
}

type store struct {
	mu    sync.Mutex
	items map[int64]*Conversation
}

func newStore() *store {
	return &store{items: make(map[int64]*Conversation)}
}

func (s *store) get(userID int64) *Conversation {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.items[userID]
}

func (s *store) put(userID int64, c *Conversation) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.items[userID] = c
}

func (s *store) del(userID int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.items, userID)
}

func (s *store) busy(userID int64) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	c := s.items[userID]
	return c != nil && c.State != StateIdle
}

func optionIndex(opts []string, idx int) (string, error) {
	if idx < 0 || idx >= len(opts) {
		return "", fmt.Errorf("option out of range")
	}
	return opts[idx], nil
}
