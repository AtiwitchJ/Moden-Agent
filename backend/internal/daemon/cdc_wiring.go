package daemon

import (
	"context"
	"encoding/json"
	"log/slog"

	"github.com/modernagent/modern-agent/backend/internal/cdc"
	"github.com/modernagent/modern-agent/backend/internal/storage/sqlite"
)

// cdcPipeline owns the running CDC poller and live-event broadcaster. The DB
// triggers write change_log; the poller tails it and fans each new event out to
// live transports such as terminal session-state subscriptions. Durable catch-up
// is a client concern; the poller only pushes live events and re-seeks to head
// on restart.
type cdcPipeline struct {
	Broadcaster *cdc.Broadcaster
	done        <-chan struct{}
}

// startCDC seeks the poller to the current head and starts its loop. It stops
// when ctx is cancelled; Stop waits for it to drain.
func startCDC(ctx context.Context, store *sqlite.Store, logger *slog.Logger) (*cdcPipeline, error) {
	bcast := cdc.NewBroadcaster()
	poller := cdc.NewPoller(store, bcast, cdc.PollerConfig{Logger: logger})
	if err := poller.SeekToHead(ctx); err != nil {
		return nil, err
	}
	return &cdcPipeline{Broadcaster: bcast, done: poller.Start(ctx)}, nil
}

// Stop waits for the poller goroutine to exit (the caller must have cancelled the
// ctx passed to startCDC).
func (p *cdcPipeline) Stop() error {
	<-p.done
	return nil
}

// SubscribeWorkCardChanges subscribes to work_card_changed CDC events from the
// shared broadcaster and forwards them as CardChangeEvent values on the returned
// channel. The channel is closed when ctx is cancelled. Each call creates a new
// independent subscription so callers can independently manage their own lifecycle.
func SubscribeWorkCardChanges(ctx context.Context, bcast *cdc.Broadcaster, logger *slog.Logger) <-chan CardChangeEvent {
	ch := make(chan CardChangeEvent, 50) // buffered so sends don't block the poller

	unsubscribe := bcast.Subscribe(func(e cdc.Event) {
		if e.Type != "work_card_changed" {
			return
		}
		var payload struct {
			CardID    string `json:"card_id"`
			ProjectID string `json:"project_id"`
			NewStatus string `json:"new_status"`
			OldStatus string `json:"old_status"`
		}
		if err := json.Unmarshal(e.Payload, &payload); err != nil {
			logger.Debug("SubscribeWorkCardChanges: unmarshal failed", "err", err)
			return
		}
		select {
		case ch <- CardChangeEvent{
			CardID:    payload.CardID,
			ProjectID: payload.ProjectID,
			NewStatus: payload.NewStatus,
			OldStatus: payload.OldStatus,
		}:
		default:
			// channel full; drop event rather than blocking the poller
		}
	})

	go func() {
		<-ctx.Done()
		unsubscribe()
		close(ch)
	}()

	return ch
}
