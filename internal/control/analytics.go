package control

import (
	"net/http"
	"time"

	"github.com/neluckoff/proxyfleet/internal/model"
)

const trafficRetention = 400 * 24 * time.Hour

type trafficPeriod struct {
	BytesUp         int64 `json:"bytesUp"`
	BytesDown       int64 `json:"bytesDown"`
	BytesTotal      int64 `json:"bytesTotal"`
	PreviousTotal   int64 `json:"previousTotal,omitempty"`
	DifferenceBytes int64 `json:"differenceBytes,omitempty"`
}

func counterDelta(previous, current int64) int64 {
	if current < 0 {
		return 0
	}
	if current >= previous {
		return current - previous
	}
	return current
}

func recordTraffic(state *model.State, now time.Time, up, down int64) {
	if up < 0 || down < 0 {
		return
	}
	state.TrafficBytesUp += up
	state.TrafficBytesDown += down
	if up == 0 && down == 0 {
		return
	}
	day := now.UTC().Truncate(24 * time.Hour)
	if len(state.Traffic) > 0 && state.Traffic[len(state.Traffic)-1].RecordedAt.Equal(day) {
		state.Traffic[len(state.Traffic)-1].BytesUp += up
		state.Traffic[len(state.Traffic)-1].BytesDown += down
	} else {
		state.Traffic = append(state.Traffic, &model.TrafficSample{RecordedAt: day, BytesUp: up, BytesDown: down})
	}

	cutoff := day.Add(-trafficRetention)
	first := 0
	for first < len(state.Traffic) && state.Traffic[first].RecordedAt.Before(cutoff) {
		first++
	}
	if first > 0 {
		state.Traffic = append([]*model.TrafficSample(nil), state.Traffic[first:]...)
	}
}

func sumTraffic(samples []*model.TrafficSample, start, end time.Time) trafficPeriod {
	period := trafficPeriod{}
	for _, sample := range samples {
		if sample.RecordedAt.Before(start) || !sample.RecordedAt.Before(end) {
			continue
		}
		period.BytesUp += sample.BytesUp
		period.BytesDown += sample.BytesDown
	}
	period.BytesTotal = period.BytesUp + period.BytesDown
	return period
}

func comparedTraffic(samples []*model.TrafficSample, start, end, previousStart time.Time) trafficPeriod {
	period := sumTraffic(samples, start, end)
	previous := sumTraffic(samples, previousStart, start)
	period.PreviousTotal = previous.BytesTotal
	period.DifferenceBytes = period.BytesTotal - previous.BytesTotal
	return period
}

func analyticsSnapshot(state *model.State, now time.Time) map[string]any {
	now = now.UTC()
	today := now.Truncate(24 * time.Hour)
	tomorrow := today.Add(24 * time.Hour)
	month := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	year := time.Date(now.Year(), time.January, 1, 0, 0, 0, 0, time.UTC)

	online, offline, pending, telegramHealthy := 0, 0, 0, 0
	var activeConnections, latency, uptime, heap, runtimeMemory, hostUsed, hostTotal int64
	var goroutines int
	var load float64
	telemetryNodes := 0
	for _, node := range state.Nodes {
		if node.LastSeenAt.IsZero() {
			pending++
		} else if nodeOnline(node, now) {
			online++
			activeConnections += node.ActiveConnections
			uptime += node.UptimeSeconds
			if node.TelegramOK {
				telegramHealthy++
				latency += node.TelegramLatencyMS
			}
			if node.MemorySysBytes > 0 || node.HostMemoryTotalBytes > 0 {
				telemetryNodes++
				heap += node.MemoryAllocBytes
				runtimeMemory += node.MemorySysBytes
				hostUsed += node.HostMemoryUsedBytes
				hostTotal += node.HostMemoryTotalBytes
				goroutines += node.Goroutines
				load += node.LoadOne
			}
		} else {
			offline++
		}
	}

	enabled, disabled, assigned, unassigned, external := 0, 0, 0, 0, 0
	for _, user := range state.Users {
		if user.Enabled {
			enabled++
		} else {
			disabled++
		}
		if proxySource(user) == "external" {
			external++
		} else if len(user.NodeIDs) > 0 {
			assigned++
		} else {
			unassigned++
		}
	}

	averageLatency := int64(0)
	if telegramHealthy > 0 {
		averageLatency = latency / int64(telegramHealthy)
	}
	averageUptime := int64(0)
	if online > 0 {
		averageUptime = uptime / int64(online)
	}
	averageLoad := float64(0)
	if telemetryNodes > 0 {
		averageLoad = load / float64(telemetryNodes)
	}

	lifetime := trafficPeriod{BytesUp: state.TrafficBytesUp, BytesDown: state.TrafficBytesDown}
	lifetime.BytesTotal = lifetime.BytesUp + lifetime.BytesDown
	proxyStats := map[string]any{
		"total": len(state.Users), "enabled": enabled, "disabled": disabled, "assigned": assigned, "unassigned": unassigned, "external": external,
	}
	return map[string]any{
		"traffic": map[string]any{
			"today":      comparedTraffic(state.Traffic, today, tomorrow, today.Add(-24*time.Hour)),
			"sevenDays":  comparedTraffic(state.Traffic, today.Add(-6*24*time.Hour), tomorrow, today.Add(-13*24*time.Hour)),
			"thirtyDays": sumTraffic(state.Traffic, today.Add(-29*24*time.Hour), tomorrow),
			"month":      sumTraffic(state.Traffic, month, tomorrow),
			"year":       sumTraffic(state.Traffic, year, tomorrow),
			"lifetime":   lifetime,
		},
		"availability": map[string]any{
			"total": len(state.Nodes), "online": online, "offline": offline, "pending": pending,
			"telegramHealthy": telegramHealthy, "averageTelegramLatencyMs": averageLatency, "activeConnections": activeConnections,
		},
		"system": map[string]any{
			"telemetryNodes": telemetryNodes, "heapBytes": heap, "runtimeMemoryBytes": runtimeMemory,
			"hostMemoryUsedBytes": hostUsed, "hostMemoryTotalBytes": hostTotal, "goroutines": goroutines,
			"averageLoad": averageLoad, "averageUptimeSeconds": averageUptime,
		},
		"proxies": proxyStats,
		"users":   proxyStats,
	}
}

func (s *server) analytics(w http.ResponseWriter, _ *http.Request) {
	state := s.store.snapshot()
	writeJSON(w, http.StatusOK, analyticsSnapshot(&state, time.Now()))
}
