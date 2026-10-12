package control

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestPlatformInfoUsesCachedGitHubStats(t *testing.T) {
	githubStatsCache.Lock()
	previousStars := githubStatsCache.stars
	previousAvailable := githubStatsCache.available
	previousFetchedAt := githubStatsCache.fetchedAt
	githubStatsCache.stars = 42
	githubStatsCache.available = true
	githubStatsCache.fetchedAt = time.Now()
	githubStatsCache.Unlock()
	defer func() {
		githubStatsCache.Lock()
		githubStatsCache.stars = previousStars
		githubStatsCache.available = previousAvailable
		githubStatsCache.fetchedAt = previousFetchedAt
		githubStatsCache.Unlock()
	}()

	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/platform", nil)
	(&server{}).platformInfo(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("platform info returned %d", response.Code)
	}
	var payload struct {
		Version     string `json:"version"`
		GitHubStars int    `json:"githubStars"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Version != version || payload.GitHubStars != 42 {
		t.Fatalf("unexpected platform info: %#v", payload)
	}
}
