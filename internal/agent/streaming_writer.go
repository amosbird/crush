package agent

import (
	"context"
	"sync"
	"time"

	"github.com/charmbracelet/crush/internal/message"
	"github.com/charmbracelet/crush/internal/pubsub"
)

// streamingWriteInterval is the minimum interval between DB writes during
// streaming. Pubsub events are still published immediately so the UI stays
// responsive.
const streamingWriteInterval = 50 * time.Millisecond

// streamingWriter wraps a message.Service to decouple pubsub publishing
// (immediate, for UI responsiveness) from DB persistence (throttled, to
// reduce JSON marshaling and SQLite write overhead during fast token
// streaming).
type streamingWriter struct {
	svc message.Service

	mu        sync.Mutex
	pending   *message.Message
	lastWrite time.Time
	timer     *time.Timer
}

func newStreamingWriter(svc message.Service) *streamingWriter {
	return &streamingWriter{svc: svc}
}

// Update publishes the message to pubsub immediately and schedules a
// debounced DB write.
func (w *streamingWriter) Update(ctx context.Context, msg message.Message) error {
	clone := msg.Clone()
	w.svc.Publish(pubsub.UpdatedEvent, clone)

	w.mu.Lock()
	defer w.mu.Unlock()

	w.pending = &msg
	now := time.Now()

	if now.Sub(w.lastWrite) >= streamingWriteInterval {
		return w.flushLocked(ctx)
	}

	if w.timer == nil {
		w.timer = time.AfterFunc(streamingWriteInterval-now.Sub(w.lastWrite), func() {
			w.mu.Lock()
			defer w.mu.Unlock()
			if w.pending != nil {
				_ = w.flushLocked(context.Background())
			}
		})
	}

	return nil
}

// Flush forces any pending message to be written to the DB.
func (w *streamingWriter) Flush(ctx context.Context) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.flushLocked(ctx)
}

func (w *streamingWriter) flushLocked(ctx context.Context) error {
	if w.timer != nil {
		w.timer.Stop()
		w.timer = nil
	}
	if w.pending == nil {
		return nil
	}
	msg := *w.pending
	w.pending = nil
	w.lastWrite = time.Now()
	return w.svc.Persist(ctx, msg)
}
