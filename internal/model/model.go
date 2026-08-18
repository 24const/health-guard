package model

import (
	"encoding/json"
	"time"
)

type CheckinSource string
type CheckinStatus string
type JobKind string
type JobStatus string

const (
	SourceScheduled CheckinSource = "scheduled"
	SourceManual    CheckinSource = "manual"

	StatusCompleted CheckinStatus = "completed"
	StatusSkipped   CheckinStatus = "skipped"

	JobKindCheckinPrompt     JobKind = "checkin_prompt"
	JobKindCravingFollowup   JobKind = "craving_followup"
	JobKindSnooze            JobKind = "snooze"

	JobPending   JobStatus = "pending"
	JobDone      JobStatus = "done"
	JobCancelled JobStatus = "cancelled"
	JobExpired   JobStatus = "expired"
)

type User struct {
	ID        int64
	Username  *string
	FirstName *string
	IsActive  bool
	CreatedAt time.Time
	UpdatedAt *time.Time
}

type Checkin struct {
	ID              int64
	UserID          int64
	CreatedAt       time.Time
	LocalDate       time.Time
	UpdatedAt       *time.Time
	Mood            *int16
	Stress          *int16
	Irritation      *int16
	SmokingCraving  *int16
	AlcoholCraving  *int16
	LifeEnjoyment   *int16
	Need            *string
	Context         *string
	CopingAction    *string
	Source          CheckinSource
	Status          CheckinStatus
}

type CravingFollowup struct {
	ID             int64
	CheckinID      int64
	CreatedAt      time.Time
	UpdatedAt      *time.Time
	SmokingCraving *int16
	AlcoholCraving *int16
}

type DailyReview struct {
	ID                 int64
	UserID             int64
	LocalDate          time.Time
	MaxSmokingCraving  *int16
	MaxAlcoholCraving  *int16
	HardestMoment      *string
	BestCopingAction   *string
	Smoked             *bool
	DrankAlcohol       *bool
	CreatedAt          time.Time
	UpdatedAt          *time.Time
}

type Job struct {
	ID        int64
	UserID    int64
	Kind      JobKind
	Payload   json.RawMessage
	FireAt    time.Time
	Status    JobStatus
	CreatedAt time.Time
}

type FollowupPayload struct {
	CheckinID int64  `json:"checkin_id"`
	Which     string `json:"which"` // smoking | alcohol | both
}

type DaySummary struct {
	Date           time.Time
	CheckinCount   int
	HasReview      bool
	Smoked         *bool
	DrankAlcohol   *bool
}

type TodayStats struct {
	CheckinCount     int
	AvgSmoking       *float64
	MaxSmoking       *int16
	AvgAlcohol       *float64
	MaxAlcohol       *int16
	AvgStress        *float64
	AvgIrritation    *float64
}

type WeekStats struct {
	CheckinCount        int
	AvgMood             *float64
	AvgStress           *float64
	AvgIrritation       *float64
	AvgSmoking          *float64
	MaxSmoking          *int16
	AvgAlcohol          *float64
	MaxAlcohol          *int16
	AvgLife             *float64
	SmokeFreeDays       int
	AlcoholFreeDays     int
	ReviewDays          int
	TopContexts         []string
	TopCoping           []string
	HighCravingCount    int
	HighCravingAvg      *float64
	FollowupAvg         *float64
	FollowupDelta       *float64
}
