package agent

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

// TaskStatus represents the status of a background task.
type TaskStatus string

const (
	TaskPending  TaskStatus = "pending"
	TaskRunning  TaskStatus = "running"
	TaskComplete TaskStatus = "complete"
	TaskFailed   TaskStatus = "failed"
)

// BackgroundTask represents a long-running task executed in the background.
type BackgroundTask struct {
	ID        string     `json:"id"`
	Name      string     `json:"name"`
	Status    TaskStatus `json:"status"`
	Result    string     `json:"result,omitempty"`
	Error     string     `json:"error,omitempty"`
	Progress  int        `json:"progress"` // 0-100
	StartedAt time.Time  `json:"started_at"`
	DoneAt    time.Time  `json:"done_at,omitempty"`
}

// BackgroundExecutor manages background task execution.
type BackgroundExecutor struct {
	mu     sync.RWMutex
	tasks  map[string]*BackgroundTask
	logger *slog.Logger
	nextID int
}

// NewBackgroundExecutor creates a new background task executor.
func NewBackgroundExecutor(logger *slog.Logger) *BackgroundExecutor {
	if logger == nil {
		logger = slog.Default()
	}
	return &BackgroundExecutor{
		tasks:  make(map[string]*BackgroundTask),
		logger: logger,
	}
}

// Submit creates a new background task and executes it asynchronously.
// The taskFn receives a progress callback that accepts 0-100.
func (be *BackgroundExecutor) Submit(ctx context.Context, name string, taskFn func(ctx context.Context, progress func(int)) (string, error)) string {
	be.mu.Lock()
	be.nextID++
	id := fmt.Sprintf("task-%d", be.nextID)
	task := &BackgroundTask{
		ID:        id,
		Name:      name,
		Status:    TaskPending,
		StartedAt: time.Now(),
	}
	be.tasks[id] = task
	be.mu.Unlock()

	be.logger.Info("background task submitted", "id", id, "name", name)

	go func() {
		be.mu.Lock()
		task.Status = TaskRunning
		be.mu.Unlock()

		progressFn := func(pct int) {
			be.mu.Lock()
			task.Progress = pct
			be.mu.Unlock()
		}

		result, err := taskFn(ctx, progressFn)

		be.mu.Lock()
		task.DoneAt = time.Now()
		if err != nil {
			task.Status = TaskFailed
			task.Error = err.Error()
			be.logger.Error("background task failed", "id", id, "err", err)
		} else {
			task.Status = TaskComplete
			task.Result = result
			task.Progress = 100
			be.logger.Info("background task completed", "id", id)
		}
		be.mu.Unlock()
	}()

	return id
}

// Get returns the current state of a task.
func (be *BackgroundExecutor) Get(id string) (*BackgroundTask, bool) {
	be.mu.RLock()
	defer be.mu.RUnlock()
	task, ok := be.tasks[id]
	if !ok {
		return nil, false
	}
	// Return a copy to prevent races.
	copy := *task
	return &copy, true
}

// List returns all tasks.
func (be *BackgroundExecutor) List() []BackgroundTask {
	be.mu.RLock()
	defer be.mu.RUnlock()
	result := make([]BackgroundTask, 0, len(be.tasks))
	for _, t := range be.tasks {
		result = append(result, *t)
	}
	return result
}

// ListActive returns tasks that are still running or pending.
func (be *BackgroundExecutor) ListActive() []BackgroundTask {
	be.mu.RLock()
	defer be.mu.RUnlock()
	var result []BackgroundTask
	for _, t := range be.tasks {
		if t.Status == TaskPending || t.Status == TaskRunning {
			result = append(result, *t)
		}
	}
	return result
}

// Clean removes completed/failed tasks older than the given duration.
func (be *BackgroundExecutor) Clean(maxAge time.Duration) int {
	be.mu.Lock()
	defer be.mu.Unlock()
	cutoff := time.Now().Add(-maxAge)
	removed := 0
	for id, t := range be.tasks {
		if (t.Status == TaskComplete || t.Status == TaskFailed) && t.DoneAt.Before(cutoff) {
			delete(be.tasks, id)
			removed++
		}
	}
	return removed
}
