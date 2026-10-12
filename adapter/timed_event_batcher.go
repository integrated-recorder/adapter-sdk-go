package adapter

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/integrated-recorder/adapter-sdk-go/protocol"
)

const (
	timedEventBatcherMaxEvents  = 32
	timedEventBatcherMaxQueued  = 64
	timedEventBatcherFlushAfter = 250 * time.Millisecond
)

var ErrTimedEventBackpressure = errors.New("timed event batcher is applying backpressure")

type queuedTimedEvent struct {
	event       protocol.TimedEvent
	resumeState json.RawMessage
}

// TimedEventBatcher batches event emissions by count or time. Flush waits for
// Core's acknowledgement, establishing the durable cursor boundary.
type TimedEventBatcher struct {
	mu       sync.Mutex
	streamID string
	emit     func(protocol.TimedEventBatch) error
	queued   []queuedTimedEvent
	inFlight chan struct{}
	err      error
	sequence uint64
	timer    *time.Timer
}

func NewTimedEventBatcher(streamID string, emit func(protocol.TimedEventBatch) error) (*TimedEventBatcher, error) {
	if streamID == "" || len(streamID) > 128 || emit == nil {
		return nil, errors.New("timed event batcher requires a stream and emitter")
	}
	return &TimedEventBatcher{streamID: streamID, emit: emit}, nil
}

// Add queues one event. Call Flush before advancing any plugin-local cursor
// that depends on the queued event; Core persists resumeState only with an
// acknowledged canonical batch.
func (b *TimedEventBatcher) Add(event protocol.TimedEvent, resumeState json.RawMessage) error {
	if err := event.Validate(); err != nil {
		return err
	}
	if len(resumeState) > protocol.MaxTimedEventResumeBytes || len(resumeState) > 0 && !json.Valid(resumeState) {
		return errors.New("timed event resume state is invalid")
	}
	b.mu.Lock()
	if b.err != nil {
		err := b.err
		b.mu.Unlock()
		return err
	}
	if len(b.queued) >= timedEventBatcherMaxQueued {
		b.mu.Unlock()
		return ErrTimedEventBackpressure
	}
	b.queued = append(b.queued, queuedTimedEvent{event: event, resumeState: append(json.RawMessage(nil), resumeState...)})
	flushNow := len(b.queued) >= timedEventBatcherMaxEvents && b.inFlight == nil
	if !flushNow && b.timer == nil {
		b.timer = time.AfterFunc(timedEventBatcherFlushAfter, func() { _ = b.Flush(context.Background()) })
	}
	b.mu.Unlock()
	if flushNow {
		return b.Flush(context.Background())
	}
	return nil
}

// Flush emits all queued events in bounded batches and returns after each
// batch has been acknowledged by Core.
func (b *TimedEventBatcher) Flush(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	for {
		b.mu.Lock()
		if b.err != nil {
			err := b.err
			b.mu.Unlock()
			return err
		}
		if b.inFlight != nil {
			wait := b.inFlight
			b.mu.Unlock()
			select {
			case <-wait:
				continue
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		if len(b.queued) == 0 {
			if b.timer != nil {
				b.timer.Stop()
				b.timer = nil
			}
			b.mu.Unlock()
			return nil
		}
		count := len(b.queued)
		if count > timedEventBatcherMaxEvents {
			count = timedEventBatcherMaxEvents
		}
		var batch protocol.TimedEventBatch
		for count > 0 {
			events := make([]protocol.TimedEvent, count)
			for i := range events {
				events[i] = b.queued[i].event
			}
			resumeState := append(json.RawMessage(nil), b.queued[count-1].resumeState...)
			batch = protocol.TimedEventBatch{StreamID: b.streamID, BatchID: fmt.Sprintf("%s-%d", b.streamID, b.sequence+1), Events: events, ResumeState: resumeState}
			if batch.Validate() == nil {
				break
			}
			count--
		}
		if count == 0 {
			err := errors.New("timed event could not fit into a protocol batch")
			b.err = err
			if b.timer != nil {
				b.timer.Stop()
				b.timer = nil
			}
			b.mu.Unlock()
			return err
		}
		b.queued = append([]queuedTimedEvent(nil), b.queued[count:]...)
		b.sequence++
		inFlight := make(chan struct{})
		b.inFlight = inFlight
		if b.timer != nil {
			b.timer.Stop()
			b.timer = nil
		}
		b.mu.Unlock()

		err := b.emit(batch)
		b.mu.Lock()
		if err != nil {
			b.err = err
		}
		b.inFlight = nil
		close(inFlight)
		if len(b.queued) > 0 && b.timer == nil {
			b.timer = time.AfterFunc(timedEventBatcherFlushAfter, func() { _ = b.Flush(context.Background()) })
		}
		b.mu.Unlock()
		if err != nil {
			return err
		}
	}
}
