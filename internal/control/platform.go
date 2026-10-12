package control

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"sync"
	"time"
)

const githubRepository = "https://github.com/neluckoff/proxyfleet"

var githubStatsCache struct {
	sync.Mutex
	stars     int
	available bool
	fetchedAt time.Time
}

func (s *server) platformInfo(w http.ResponseWriter, r *http.Request) {
	stars, available := githubStars(r.Context())
	response := map[string]any{
		"version":   version,
		"githubUrl": githubRepository,
	}
	if available {
		response["githubStars"] = stars
	}
	writeJSON(w, http.StatusOK, response)
}

func githubStars(parent context.Context) (int, bool) {
	githubStatsCache.Lock()
	defer githubStatsCache.Unlock()
	if time.Since(githubStatsCache.fetchedAt) < 15*time.Minute {
		return githubStatsCache.stars, githubStatsCache.available
	}
	githubStatsCache.fetchedAt = time.Now()

	ctx, cancel := context.WithTimeout(parent, 3*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.github.com/repos/neluckoff/proxyfleet", nil)
	if err != nil {
		return githubStatsCache.stars, githubStatsCache.available
	}
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("User-Agent", "ProxyFleet/"+version)
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return githubStatsCache.stars, githubStatsCache.available
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return githubStatsCache.stars, githubStatsCache.available
	}
	var payload struct {
		Stars int `json:"stargazers_count"`
	}
	if json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&payload) != nil {
		return githubStatsCache.stars, githubStatsCache.available
	}
	githubStatsCache.stars = payload.Stars
	githubStatsCache.available = true
	return payload.Stars, true
}
