// Package schedule provides a simple task scheduling system.
//
// Scheduler runs tasks at scheduled times using time.Timer. Each task can be
// one-shot (RunAt) or recurring (Interval). The InMemoryScheduler is
// process-local; production deployments can implement the Scheduler interface
// with persistent backends.
package schedule

import (
	"context"
	"fmt"
	"sync"
	"time"

	agentscope "github.com/alanfokco/agentscope-go/pkg/agentscope"
)

// TaskStatus represents the lifecycle state of a scheduled task.
type TaskStatus string

const (
	StatusPending   TaskStatus = "pending"
	StatusRunning   TaskStatus = "running"
	StatusCompleted TaskStatus = "completed"
	StatusCanceled  TaskStatus = "canceled"
	StatusFailed    TaskStatus = "failed"
)

// TaskFunc is the function signature for scheduled task execution.
type TaskFunc func(ctx context.Context, task *Task) error

// Task describes a scheduled unit of work.
type Task struct {
	ID          string        `json:"id"`
	Name        string        `json:"name"`
	Description string        `json:"description,omitempty"`
	AgentName   string        `json:"agent_name,omitempty"`
	Input       string        `json:"input,omitempty"`
	RunAt       time.Time     `json:"run_at,omitempty"`
	Interval    time.Duration `json:"interval,omitempty"`
	Status      TaskStatus    `json:"status"`
	CreatedAt   time.Time     `json:"created_at"`
	LastRunAt   time.Time     `json:"last_run_at,omitempty"`
	Error       string        `json:"error,omitempty"`
}

// Scheduler manages scheduled task execution.
type Scheduler interface {
	Schedule(ctx context.Context, task *Task, fn TaskFunc) (string, error)
	Cancel(ctx context.Context, taskID string) error
	Get(ctx context.Context, taskID string) (*Task, error)
	List(ctx context.Context) ([]*Task, error)
	Close() error
}

// InMemoryScheduler uses time.Timer/time.Ticker for process-local scheduling.
type InMemoryScheduler struct {
	mu      sync.Mutex
	tasks   map[string]*scheduledEntry
	closed  bool
	closeCh chan struct{}
}

type scheduledEntry struct {
	task   *Task
	fn     TaskFunc
	cancel context.CancelFunc
}

// NewInMemoryScheduler creates a new scheduler.
func NewInMemoryScheduler() *InMemoryScheduler {
	return &InMemoryScheduler{
		tasks:   make(map[string]*scheduledEntry),
		closeCh: make(chan struct{}),
	}
}

// Schedule registers a task for execution. If task.RunAt is set, the task runs
// once at that time. If task.Interval > 0, it runs repeatedly. If neither is
// set, the task runs immediately.
func (s *InMemoryScheduler) Schedule(ctx context.Context, task *Task, fn TaskFunc) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return "", fmt.Errorf("scheduler: closed")
	}

	if task.ID == "" {
		task.ID = agentscope.GenerateID()
	}
	task.Status = StatusPending
	task.CreatedAt = time.Now()

	taskCtx, cancel := context.WithCancel(context.Background())

	entry := &scheduledEntry{
		task:   task,
		fn:     fn,
		cancel: cancel,
	}
	s.tasks[task.ID] = entry

	go s.run(taskCtx, entry)

	return task.ID, nil
}

func (s *InMemoryScheduler) run(ctx context.Context, entry *scheduledEntry) {
	task := entry.task

	if task.Interval > 0 {
		s.runRecurring(ctx, entry)
		return
	}

	if !task.RunAt.IsZero() {
		delay := time.Until(task.RunAt)
		if delay > 0 {
			timer := time.NewTimer(delay)
			defer timer.Stop()
			select {
			case <-timer.C:
			case <-ctx.Done():
				s.setStatus(task.ID, StatusCanceled)
				return
			case <-s.closeCh:
				s.setStatus(task.ID, StatusCanceled)
				return
			}
		}
	}

	s.executeOnce(ctx, entry)
}

func (s *InMemoryScheduler) runRecurring(ctx context.Context, entry *scheduledEntry) {
	task := entry.task

	if !task.RunAt.IsZero() {
		delay := time.Until(task.RunAt)
		if delay > 0 {
			timer := time.NewTimer(delay)
			defer timer.Stop()
			select {
			case <-timer.C:
			case <-ctx.Done():
				s.setStatus(task.ID, StatusCanceled)
				return
			case <-s.closeCh:
				s.setStatus(task.ID, StatusCanceled)
				return
			}
		}
	}

	ticker := time.NewTicker(task.Interval)
	defer ticker.Stop()

	s.executeOnce(ctx, entry)

	for {
		select {
		case <-ticker.C:
			s.executeOnce(ctx, entry)
		case <-ctx.Done():
			s.setStatus(task.ID, StatusCanceled)
			return
		case <-s.closeCh:
			s.setStatus(task.ID, StatusCanceled)
			return
		}
	}
}

func (s *InMemoryScheduler) executeOnce(ctx context.Context, entry *scheduledEntry) {
	s.setStatus(entry.task.ID, StatusRunning)

	err := entry.fn(ctx, entry.task)

	s.mu.Lock()
	defer s.mu.Unlock()

	entry.task.LastRunAt = time.Now()
	if err != nil {
		entry.task.Status = StatusFailed
		entry.task.Error = err.Error()
	} else if entry.task.Interval == 0 {
		entry.task.Status = StatusCompleted
	} else {
		entry.task.Status = StatusPending
	}
}

func (s *InMemoryScheduler) setStatus(taskID string, status TaskStatus) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if e, ok := s.tasks[taskID]; ok {
		e.task.Status = status
	}
}

// Cancel stops a scheduled task.
func (s *InMemoryScheduler) Cancel(_ context.Context, taskID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	entry, ok := s.tasks[taskID]
	if !ok {
		return fmt.Errorf("scheduler: task %q not found", taskID)
	}

	entry.cancel()
	entry.task.Status = StatusCanceled
	return nil
}

// Get returns a task by ID.
func (s *InMemoryScheduler) Get(_ context.Context, taskID string) (*Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	entry, ok := s.tasks[taskID]
	if !ok {
		return nil, fmt.Errorf("scheduler: task %q not found", taskID)
	}
	cp := *entry.task
	return &cp, nil
}

// List returns all tasks.
func (s *InMemoryScheduler) List(_ context.Context) ([]*Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	tasks := make([]*Task, 0, len(s.tasks))
	for _, e := range s.tasks {
		tasks = append(tasks, e.task)
	}
	return tasks, nil
}

// Close shuts down the scheduler and cancels all running tasks.
func (s *InMemoryScheduler) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return nil
	}
	s.closed = true
	close(s.closeCh)

	for _, e := range s.tasks {
		e.cancel()
		if e.task.Status == StatusPending || e.task.Status == StatusRunning {
			e.task.Status = StatusCanceled
		}
	}

	return nil
}

// Compile-time interface check.
var _ Scheduler = (*InMemoryScheduler)(nil)
