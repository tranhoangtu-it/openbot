package tool

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"
	"openbot/internal/domain"
)

type CronScheduler struct {
	tasks    map[string]*ScheduledTask
	bus      domain.MessageBus
	logger   *slog.Logger
	mu       sync.RWMutex
	stopCh   chan struct{}
	stopOnce sync.Once
}

type ScheduledTask struct {
	ID         string
	Name       string
	Message    string    // Message to send to agent
	IntervalS  int       // Interval in seconds
	Channel    string    // Target channel
	ChatID     string    // Target chat ID
	Enabled    bool
	LastRun    time.Time
	NextRun    time.Time
}

func NewCronScheduler(bus domain.MessageBus, logger *slog.Logger) *CronScheduler {
	return &CronScheduler{
		tasks:  make(map[string]*ScheduledTask),
		bus:    bus,
		logger: logger,
		stopCh: make(chan struct{}),
	}
}

func (cs *CronScheduler) AddTask(task ScheduledTask) {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	task.NextRun = time.Now().Add(time.Duration(task.IntervalS) * time.Second)
	cs.tasks[task.ID] = &task
	cs.logger.Info("cron task added", "id", task.ID, "name", task.Name, "interval", task.IntervalS)
}

func (cs *CronScheduler) RemoveTask(id string) {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	delete(cs.tasks, id)
	cs.logger.Info("cron task removed", "id", id)
}

func (cs *CronScheduler) ListTasks() []ScheduledTask {
	cs.mu.RLock()
	defer cs.mu.RUnlock()
	tasks := make([]ScheduledTask, 0, len(cs.tasks))
	for _, t := range cs.tasks {
		tasks = append(tasks, *t)
	}
	return tasks
}

func (cs *CronScheduler) Start(ctx context.Context) {
	cs.logger.Info("cron scheduler started")
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			cs.logger.Info("cron scheduler stopping")
			return
		case <-cs.stopCh:
			return
		case now := <-ticker.C:
			cs.checkAndExecute(now)
		}
	}
}

// Stop halts the cron scheduler. Safe to call multiple times.
func (cs *CronScheduler) Stop() {
	cs.stopOnce.Do(func() {
		close(cs.stopCh)
	})
}

func (cs *CronScheduler) checkAndExecute(now time.Time) {
	cs.mu.Lock()
	defer cs.mu.Unlock()

	for _, task := range cs.tasks {
		if !task.Enabled {
			continue
		}
		if now.After(task.NextRun) {
			cs.logger.Info("executing cron task", "id", task.ID, "name", task.Name)
			cs.bus.Publish(domain.InboundMessage{
				Channel:   task.Channel,
				ChatID:    task.ChatID,
				SenderID:  "cron:" + task.ID,
				Content:   task.Message,
				Timestamp: now,
			})
			task.LastRun = now
			task.NextRun = now.Add(time.Duration(task.IntervalS) * time.Second)
		}
	}
}

type CronTool struct {
	scheduler *CronScheduler
}

func NewCronTool(scheduler *CronScheduler) *CronTool {
	return &CronTool{scheduler: scheduler}
}

func (t *CronTool) Name() string { return "cron" }
func (t *CronTool) Description() string {
	return "Manage scheduled tasks. Actions: 'list' (show all tasks), 'add' (create a new task with name, message, interval_seconds, channel, chat_id), 'remove' (delete a task by id)."
}
func (t *CronTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"action":           map[string]any{"type": "string", "description": "Action: list, add, remove"},
			"id":               map[string]any{"type": "string", "description": "Task ID (for remove)"},
			"name":             map[string]any{"type": "string", "description": "Task name (for add)"},
			"message":          map[string]any{"type": "string", "description": "Message to send when triggered (for add)"},
			"interval_seconds": map[string]any{"type": "number", "description": "Interval in seconds (for add)"},
			"channel":          map[string]any{"type": "string", "description": "Target channel (for add, default: telegram)"},
			"chat_id":          map[string]any{"type": "string", "description": "Target chat ID (for add)"},
		},
		"required": []string{"action"},
	}
}

func (t *CronTool) Execute(ctx context.Context, args map[string]any) (string, error) {
	action := ArgsString(args, "action")
	switch action {
	case "list":
		tasks := t.scheduler.ListTasks()
		if len(tasks) == 0 {
			return "No scheduled tasks.", nil
		}
		var lines []string
		for _, task := range tasks {
			status := "enabled"
			if !task.Enabled {
				status = "disabled"
			}
			lines = append(lines, fmt.Sprintf("- [%s] %s: \"%s\" every %ds (%s) next: %s",
				task.ID, task.Name, task.Message, task.IntervalS, status, task.NextRun.Format(time.RFC3339)))
		}
		return strings.Join(lines, "\n"), nil

	case "add":
		name := ArgsString(args, "name")
		message := ArgsString(args, "message")
		intervalRaw, ok := args["interval_seconds"].(float64)
		if !ok || intervalRaw <= 0 {
			return "Error: interval_seconds must be a positive number.", nil
		}
		interval := int(intervalRaw)
		ch := ArgsString(args, "channel")
		chatID := ArgsString(args, "chat_id")

		if name == "" || message == "" || interval <= 0 {
			return "Error: name, message, and interval_seconds are required for add.", nil
		}
		if ch == "" {
			ch = "telegram"
		}

		id := fmt.Sprintf("task_%d", time.Now().UnixMilli())
		t.scheduler.AddTask(ScheduledTask{
			ID:        id,
			Name:      name,
			Message:   message,
			IntervalS: interval,
			Channel:   ch,
			ChatID:    chatID,
			Enabled:   true,
		})
		return fmt.Sprintf("Task created: %s (ID: %s), runs every %ds", name, id, interval), nil

	case "remove":
		id := ArgsString(args, "id")
		if id == "" {
			return "Error: id is required for remove.", nil
		}
		t.scheduler.RemoveTask(id)
		return fmt.Sprintf("Task removed: %s", id), nil

	default:
		return "Unknown action. Use: list, add, remove.", nil
	}
}
