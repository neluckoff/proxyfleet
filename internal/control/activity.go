package control

import (
	"net/http"
	"time"

	"github.com/neluckoff/proxyfleet/internal/model"
)

const (
	activityBucket    = 5 * time.Minute
	activityRetention = 7 * 24 * time.Hour
	activityWindow    = 24 * time.Hour
)

func activitySnapshot(state *model.State, now time.Time) *model.ActivitySample {
	sample := &model.ActivitySample{RecordedAt: now.UTC().Truncate(activityBucket)}
	for _, node := range state.Nodes {
		if !nodeOnline(node, now) {
			continue
		}
		sample.OnlineNodes++
		sample.ActiveConnections += node.ActiveConnections
	}
	return sample
}

func recordActivitySample(state *model.State, now time.Time) {
	sample := activitySnapshot(state, now)
	if len(state.Activity) > 0 && state.Activity[len(state.Activity)-1].RecordedAt.Equal(sample.RecordedAt) {
		state.Activity[len(state.Activity)-1] = sample
	} else {
		state.Activity = append(state.Activity, sample)
	}

	cutoff := now.Add(-activityRetention)
	first := 0
	for first < len(state.Activity) && state.Activity[first].RecordedAt.Before(cutoff) {
		first++
	}
	if first > 0 {
		state.Activity = append([]*model.ActivitySample(nil), state.Activity[first:]...)
	}
}

func (s *server) activity(w http.ResponseWriter, _ *http.Request) {
	state := s.store.snapshot()
	now := time.Now().UTC()
	cutoff := now.Add(-activityWindow)
	samples := make([]*model.ActivitySample, 0, len(state.Activity)+1)
	for _, sample := range state.Activity {
		if !sample.RecordedAt.Before(cutoff) {
			samples = append(samples, sample)
		}
	}

	current := activitySnapshot(&state, now)
	if len(samples) > 0 && samples[len(samples)-1].RecordedAt.Equal(current.RecordedAt) {
		samples[len(samples)-1] = current
	} else {
		samples = append(samples, current)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"periodHours":   24,
		"bucketMinutes": int(activityBucket / time.Minute),
		"samples":       samples,
	})
}
