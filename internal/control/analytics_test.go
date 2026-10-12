package control

import (
	"testing"
	"time"

	"github.com/neluckoff/proxyfleet/internal/model"
)

func TestCounterDeltaHandlesAgentRestart(t *testing.T) {
	if delta := counterDelta(1000, 1450); delta != 450 {
		t.Fatalf("expected regular delta 450, got %d", delta)
	}
	if delta := counterDelta(1000, 125); delta != 125 {
		t.Fatalf("expected reset counter value 125, got %d", delta)
	}
}

func TestRecordTrafficAggregatesDailySamples(t *testing.T) {
	now := time.Date(2026, time.October, 12, 12, 0, 0, 0, time.UTC)
	state := model.State{}
	recordTraffic(&state, now, 100, 300)
	recordTraffic(&state, now.Add(time.Hour), 50, 75)
	recordTraffic(&state, now.Add(24*time.Hour), 25, 50)

	if state.TrafficBytesUp != 175 || state.TrafficBytesDown != 425 {
		t.Fatalf("unexpected lifetime counters: up=%d down=%d", state.TrafficBytesUp, state.TrafficBytesDown)
	}
	if len(state.Traffic) != 2 || state.Traffic[0].BytesUp != 150 || state.Traffic[0].BytesDown != 375 {
		t.Fatalf("unexpected daily traffic: %#v", state.Traffic)
	}
}

func TestAnalyticsSnapshotGroupsInfrastructureMetrics(t *testing.T) {
	now := time.Date(2026, time.October, 12, 12, 0, 0, 0, time.UTC)
	state := model.State{
		Nodes: []*model.Node{
			{LastSeenAt: now.Add(-5 * time.Second), TelegramOK: true, TelegramLatencyMS: 80, UptimeSeconds: 3600, ActiveConnections: 3, MemoryAllocBytes: 100, MemorySysBytes: 200, HostMemoryUsedBytes: 300, HostMemoryTotalBytes: 600, Goroutines: 8, LoadOne: .4},
			{LastSeenAt: now.Add(-time.Hour)},
			{},
		},
		Users: []*model.ProxyUser{
			{Enabled: true, NodeIDs: []string{"one"}},
			{Enabled: false},
		},
		Traffic: []*model.TrafficSample{
			{RecordedAt: now.Truncate(24 * time.Hour), BytesUp: 100, BytesDown: 200},
			{RecordedAt: now.Add(-24 * time.Hour).Truncate(24 * time.Hour), BytesUp: 25, BytesDown: 75},
		},
		TrafficBytesUp: 500, TrafficBytesDown: 1000,
	}

	result := analyticsSnapshot(&state, now)
	availability := result["availability"].(map[string]any)
	if availability["online"] != 1 || availability["offline"] != 1 || availability["pending"] != 1 {
		t.Fatalf("unexpected availability: %#v", availability)
	}
	traffic := result["traffic"].(map[string]any)
	today := traffic["today"].(trafficPeriod)
	if today.BytesTotal != 300 || today.PreviousTotal != 100 || today.DifferenceBytes != 200 {
		t.Fatalf("unexpected today traffic: %#v", today)
	}
	system := result["system"].(map[string]any)
	if system["telemetryNodes"] != 1 || system["goroutines"] != 8 {
		t.Fatalf("unexpected system metrics: %#v", system)
	}
}
