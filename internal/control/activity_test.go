package control

import (
	"testing"
	"time"

	"github.com/neluckoff/proxyfleet/internal/model"
)

func TestRecordActivitySampleAggregatesBuckets(t *testing.T) {
	now := time.Date(2026, time.October, 12, 10, 2, 0, 0, time.UTC)
	state := model.State{Nodes: []*model.Node{
		{LastSeenAt: now.Add(-5 * time.Second), ActiveConnections: 7},
		{LastSeenAt: now.Add(-time.Minute), ActiveConnections: 11},
	}}

	recordActivitySample(&state, now)
	if len(state.Activity) != 1 || state.Activity[0].OnlineNodes != 1 || state.Activity[0].ActiveConnections != 7 {
		t.Fatalf("unexpected first activity sample: %#v", state.Activity)
	}

	state.Nodes[0].ActiveConnections = 3
	state.Nodes[0].LastSeenAt = now.Add(2*time.Minute - 5*time.Second)
	recordActivitySample(&state, now.Add(2*time.Minute))
	if len(state.Activity) != 1 || state.Activity[0].ActiveConnections != 3 {
		t.Fatalf("same bucket was not updated: %#v", state.Activity)
	}

	recordActivitySample(&state, now.Add(5*time.Minute))
	if len(state.Activity) != 2 {
		t.Fatalf("new bucket was not appended: %#v", state.Activity)
	}
}
