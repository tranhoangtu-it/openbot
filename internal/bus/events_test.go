package bus

import (
	"log/slog"
	"os"
	"sync/atomic"
	"testing"
	"time"
)

func testEBLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
}

func TestEventBus_EmitAndReceive(t *testing.T) {
	eb := NewEventBus(testEBLogger())

	var received int32
	eb.On("test.event", func(e Event) {
		atomic.AddInt32(&received, 1)
	})

	eb.Emit(Event{Type: "test.event", Payload: map[string]any{"key": "value"}})

	if atomic.LoadInt32(&received) != 1 {
		t.Errorf("expected 1 event received, got %d", received)
	}
}

func TestEventBus_WildcardHandler(t *testing.T) {
	eb := NewEventBus(testEBLogger())

	var count int32
	eb.On("*", func(e Event) {
		atomic.AddInt32(&count, 1)
	})

	eb.Emit(Event{Type: "event.a"})
	eb.Emit(Event{Type: "event.b"})

	if atomic.LoadInt32(&count) != 2 {
		t.Errorf("expected 2, got %d", count)
	}
}

func TestEventBus_Off(t *testing.T) {
	eb := NewEventBus(testEBLogger())

	var count int32
	id := eb.On("test.event", func(e Event) {
		atomic.AddInt32(&count, 1)
	})

	eb.Emit(Event{Type: "test.event"})
	eb.Off("test.event", id)
	eb.Emit(Event{Type: "test.event"})

	if atomic.LoadInt32(&count) != 1 {
		t.Errorf("expected 1 after unsubscribe, got %d", count)
	}
}

func TestEventBus_Replay(t *testing.T) {
	eb := NewEventBus(testEBLogger())

	eb.Emit(Event{Type: "a"})
	eb.Emit(Event{Type: "b"})
	eb.Emit(Event{Type: "a"})

	events := eb.Replay("a", time.Time{})
	if len(events) != 2 {
		t.Errorf("expected 2 'a' events, got %d", len(events))
	}

	allEvents := eb.Replay("*", time.Time{})
	if len(allEvents) != 3 {
		t.Errorf("expected 3 total events, got %d", len(allEvents))
	}
}

func TestEventBus_ReplaySince(t *testing.T) {
	eb := NewEventBus(testEBLogger())

	eb.Emit(Event{Type: "old", Timestamp: time.Now().Add(-time.Hour)})
	threshold := time.Now()
	eb.Emit(Event{Type: "new"})

	events := eb.Replay("*", threshold)
	if len(events) != 1 {
		t.Errorf("expected 1 event since threshold, got %d", len(events))
	}
}

func TestEventBus_HistoryLimit(t *testing.T) {
	eb := NewEventBus(testEBLogger())
	eb.maxHistory = 5

	for i := 0; i < 10; i++ {
		eb.Emit(Event{Type: "test"})
	}

	if eb.HistoryLen() != 5 {
		t.Errorf("expected 5, got %d", eb.HistoryLen())
	}
}

func TestEventBus_PanicRecovery(t *testing.T) {
	eb := NewEventBus(testEBLogger())

	eb.On("panic", func(e Event) {
		panic("test panic")
	})

	// Should not panic the caller
	eb.Emit(Event{Type: "panic"})
}

func TestEventBus_EmitAsync(t *testing.T) {
	eb := NewEventBus(testEBLogger())

	var received int32
	eb.On("async", func(e Event) {
		atomic.AddInt32(&received, 1)
	})

	eb.EmitAsync(Event{Type: "async"})
	time.Sleep(50 * time.Millisecond)

	if atomic.LoadInt32(&received) != 1 {
		t.Errorf("expected 1, got %d", received)
	}
}

func TestEventBus_MultipleHandlers(t *testing.T) {
	eb := NewEventBus(testEBLogger())

	var count int32
	eb.On("test", func(e Event) { atomic.AddInt32(&count, 1) })
	eb.On("test", func(e Event) { atomic.AddInt32(&count, 1) })
	eb.On("test", func(e Event) { atomic.AddInt32(&count, 1) })

	eb.Emit(Event{Type: "test"})

	if atomic.LoadInt32(&count) != 3 {
		t.Errorf("expected 3 handlers called, got %d", count)
	}
}

func TestEventBus_TimestampAutoSet(t *testing.T) {
	eb := NewEventBus(testEBLogger())

	before := time.Now()
	eb.Emit(Event{Type: "test"})

	events := eb.Replay("test", before.Add(-time.Second))
	if len(events) == 0 {
		t.Fatal("expected at least 1 event")
	}
	if events[0].Timestamp.IsZero() {
		t.Error("timestamp should be auto-set")
	}
}
