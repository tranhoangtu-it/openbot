package agent

import (
	"context"
	"fmt"
	"testing"
	"time"
)

func TestBackgroundExecutor_Submit(t *testing.T) {
	be := NewBackgroundExecutor(testLogger())

	id := be.Submit(context.Background(), "test task", func(ctx context.Context, progress func(int)) (string, error) {
		progress(50)
		return "done", nil
	})

	if id == "" {
		t.Fatal("expected non-empty task ID")
	}

	// Wait for completion.
	time.Sleep(100 * time.Millisecond)

	task, ok := be.Get(id)
	if !ok {
		t.Fatal("task not found")
	}
	if task.Status != TaskComplete {
		t.Errorf("expected complete, got %s", task.Status)
	}
	if task.Result != "done" {
		t.Errorf("expected result 'done', got %q", task.Result)
	}
	if task.Progress != 100 {
		t.Errorf("expected progress 100, got %d", task.Progress)
	}
}

func TestBackgroundExecutor_SubmitFailed(t *testing.T) {
	be := NewBackgroundExecutor(testLogger())

	id := be.Submit(context.Background(), "failing task", func(ctx context.Context, progress func(int)) (string, error) {
		return "", fmt.Errorf("something went wrong")
	})

	time.Sleep(100 * time.Millisecond)

	task, _ := be.Get(id)
	if task.Status != TaskFailed {
		t.Errorf("expected failed, got %s", task.Status)
	}
	if task.Error == "" {
		t.Error("expected error message")
	}
}

func TestBackgroundExecutor_List(t *testing.T) {
	be := NewBackgroundExecutor(testLogger())

	be.Submit(context.Background(), "task1", func(ctx context.Context, progress func(int)) (string, error) {
		return "ok", nil
	})
	be.Submit(context.Background(), "task2", func(ctx context.Context, progress func(int)) (string, error) {
		return "ok", nil
	})

	time.Sleep(100 * time.Millisecond)

	tasks := be.List()
	if len(tasks) != 2 {
		t.Errorf("expected 2 tasks, got %d", len(tasks))
	}
}

func TestBackgroundExecutor_ListActive(t *testing.T) {
	be := NewBackgroundExecutor(testLogger())

	// Submit a blocking task.
	done := make(chan struct{})
	be.Submit(context.Background(), "blocking", func(ctx context.Context, progress func(int)) (string, error) {
		<-done
		return "ok", nil
	})

	time.Sleep(50 * time.Millisecond)

	active := be.ListActive()
	if len(active) != 1 {
		t.Errorf("expected 1 active task, got %d", len(active))
	}

	close(done)
	time.Sleep(50 * time.Millisecond)

	active = be.ListActive()
	if len(active) != 0 {
		t.Errorf("expected 0 active tasks after completion, got %d", len(active))
	}
}

func TestBackgroundExecutor_Clean(t *testing.T) {
	be := NewBackgroundExecutor(testLogger())

	be.Submit(context.Background(), "old task", func(ctx context.Context, progress func(int)) (string, error) {
		return "ok", nil
	})

	time.Sleep(100 * time.Millisecond)

	// Clean with 0 duration (remove everything completed)
	removed := be.Clean(0)
	if removed != 1 {
		t.Errorf("expected 1 removed, got %d", removed)
	}

	tasks := be.List()
	if len(tasks) != 0 {
		t.Errorf("expected 0 tasks after clean, got %d", len(tasks))
	}
}

func TestBackgroundExecutor_Get_NotFound(t *testing.T) {
	be := NewBackgroundExecutor(testLogger())
	_, ok := be.Get("nonexistent")
	if ok {
		t.Error("expected not found")
	}
}

func TestBackgroundExecutor_UniqueIDs(t *testing.T) {
	be := NewBackgroundExecutor(testLogger())
	ids := make(map[string]bool)

	for i := 0; i < 10; i++ {
		id := be.Submit(context.Background(), "task", func(ctx context.Context, progress func(int)) (string, error) {
			return "ok", nil
		})
		if ids[id] {
			t.Errorf("duplicate task ID: %s", id)
		}
		ids[id] = true
	}
}

func TestBackgroundExecutor_Progress(t *testing.T) {
	be := NewBackgroundExecutor(testLogger())

	progressCh := make(chan struct{})
	id := be.Submit(context.Background(), "progress task", func(ctx context.Context, progress func(int)) (string, error) {
		progress(25)
		close(progressCh)
		time.Sleep(50 * time.Millisecond)
		progress(75)
		return "ok", nil
	})

	<-progressCh
	task, _ := be.Get(id)
	if task.Progress < 25 {
		t.Errorf("expected progress >= 25, got %d", task.Progress)
	}
}
