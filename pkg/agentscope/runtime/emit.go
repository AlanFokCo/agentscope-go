package runtime

import (
	"context"

	"github.com/alanfokco/agentscope-go/pkg/agentscope/event"
)

// emitEvent forwards ev to out without blocking past context cancellation.
//
// Fast path: deliver immediately when the buffer has room (guaranteeing terminal
// events reach a consumer that is still draining). Slow path: if the send would
// block, honor ctx.Done so an abandoned consumer (e.g. an HTTP client that
// disconnected, cancelling the request context) can no longer wedge the
// forwarding goroutine and the upstream loop feeding it.
func emitEvent(ctx context.Context, out chan<- event.Event, ev event.Event) {
	select {
	case out <- ev:
		return
	default:
	}
	select {
	case out <- ev:
	case <-ctx.Done():
	}
}
