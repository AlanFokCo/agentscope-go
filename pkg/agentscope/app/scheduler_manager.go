package app

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/alanfokco/agentscope-go/pkg/agentscope/schedule"
	"github.com/google/uuid"
	"github.com/sirupsen/logrus"
)

// SchedulerManager integrates the schedule package with the app layer.
// It creates scheduled tasks that trigger chat runs.
type SchedulerManager struct {
	mu        sync.RWMutex
	scheduler schedule.Scheduler
	chatSvc   *ChatService
	records   map[string]*ScheduleRecord
}

// ScheduleRecord tracks a managed schedule.
type ScheduleRecord struct {
	ID        string    `json:"id"`
	SessionID string    `json:"session_id"`
	CronExpr  string    `json:"cron_expr,omitempty"`
	Input     string    `json:"input"`
	TaskID    string    `json:"task_id"` // from scheduler
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
}

// NewSchedulerManager creates a scheduler manager.
func NewSchedulerManager(scheduler schedule.Scheduler, chatSvc *ChatService) *SchedulerManager {
	return &SchedulerManager{
		scheduler: scheduler,
		chatSvc:   chatSvc,
		records:   make(map[string]*ScheduleRecord),
	}
}

// Create creates a new scheduled task.
func (m *SchedulerManager) Create(ctx context.Context, req CreateScheduleRequest) (*ScheduleRecord, error) {
	record := &ScheduleRecord{
		ID:        uuid.NewString(),
		SessionID: req.SessionID,
		CronExpr:  req.CronExpr,
		Input:     req.Input,
		Status:    "active",
		CreatedAt: time.Now(),
	}

	task := &schedule.Task{
		Name:  fmt.Sprintf("schedule_%s", record.ID),
		Input: req.Input,
	}

	if req.CronExpr != "" {
		task.Interval = parseCronInterval(req.CronExpr)
	} else {
		task.RunAt = time.Now().Add(time.Second)
	}

	chatSvc := m.chatSvc
	sessionID := req.SessionID
	fn := schedule.TaskFunc(func(ctx context.Context, t *schedule.Task) error {
		_, err := chatSvc.Chat(ctx, sessionID, t.Input)
		if err != nil {
			logrus.WithError(err).WithField("schedule", record.ID).
				Warn("scheduled chat failed")
		}
		return err
	})

	taskID, err := m.scheduler.Schedule(ctx, task, fn)
	if err != nil {
		return nil, fmt.Errorf("schedule task: %w", err)
	}
	record.TaskID = taskID

	m.mu.Lock()
	m.records[record.ID] = record
	m.mu.Unlock()
	return record, nil
}

// Cancel cancels a scheduled task.
func (m *SchedulerManager) Cancel(ctx context.Context, id string) error {
	m.mu.Lock()
	record, ok := m.records[id]
	if !ok {
		m.mu.Unlock()
		return fmt.Errorf("schedule %s not found", id)
	}
	record.Status = "cancelled"
	m.mu.Unlock()

	return m.scheduler.Cancel(ctx, record.TaskID)
}

// Get returns a schedule record by ID.
func (m *SchedulerManager) Get(id string) (*ScheduleRecord, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	r, ok := m.records[id]
	return r, ok
}

// List returns all schedule records.
func (m *SchedulerManager) List() []*ScheduleRecord {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make([]*ScheduleRecord, 0, len(m.records))
	for _, r := range m.records {
		result = append(result, r)
	}
	return result
}

// parseCronInterval is a simple cron interval parser.
// For a full cron implementation, integrate a library like robfig/cron.
func parseCronInterval(expr string) time.Duration {
	switch expr {
	case "@hourly":
		return time.Hour
	case "@daily":
		return 24 * time.Hour
	case "@every_5m":
		return 5 * time.Minute
	case "@every_10m":
		return 10 * time.Minute
	case "@every_30m":
		return 30 * time.Minute
	default:
		return time.Hour
	}
}
