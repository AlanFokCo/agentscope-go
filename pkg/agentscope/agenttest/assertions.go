package agenttest

import (
	"testing"

	"github.com/alanfokco/agentscope-go/pkg/agentscope/event"
)

// AssertEventPresent fails the test if no event of the given type is found.
func AssertEventPresent(t *testing.T, events []event.Event, eventType event.EventType) {
	t.Helper()
	for _, ev := range events {
		if ev.GetEventType() == eventType {
			return
		}
	}
	t.Errorf("expected event type %s not found in %d events", eventType, len(events))
}

// AssertEventAbsent fails the test if an event of the given type is found.
func AssertEventAbsent(t *testing.T, events []event.Event, eventType event.EventType) {
	t.Helper()
	for _, ev := range events {
		if ev.GetEventType() == eventType {
			t.Errorf("unexpected event type %s found", eventType)
			return
		}
	}
}

// AssertNoMissingToolResults checks that every ToolResultStartEvent has a
// matching ToolResultEndEvent with the same ToolCallID.
func AssertNoMissingToolResults(t *testing.T, events []event.Event) {
	t.Helper()
	started := make(map[string]bool)
	ended := make(map[string]bool)

	for _, ev := range events {
		switch e := ev.(type) {
		case event.ToolResultStartEvent:
			started[e.ToolCallID] = true
		case event.ToolResultEndEvent:
			ended[e.ToolCallID] = true
		}
	}

	for id := range started {
		if !ended[id] {
			t.Errorf("tool result started but not ended: %s", id)
		}
	}
}

// CollectEvents drains an event channel into a slice.
func CollectEvents(ch <-chan event.Event) []event.Event {
	var events []event.Event
	for ev := range ch {
		events = append(events, ev)
	}
	return events
}
