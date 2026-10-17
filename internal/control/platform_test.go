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
	githubReleaseCache.Lock()
	previousRelease := githubReleaseCache.latestRelease
	previousReleaseAvailable := githubReleaseCache.releaseAvailable
	previousReleaseFetchedAt := githubReleaseCache.releaseFetchedAt
	githubReleaseCache.latestRelease = githubRelease{Version: "0.6.0", Tag: "v0.6.0", Name: "ProxyFleet 0.6.0", URL: "https://github.com/neluckoff/proxyfleet/releases/tag/v0.6.0"}
	githubReleaseCache.releaseAvailable = true
	githubReleaseCache.releaseFetchedAt = time.Now()
	githubReleaseCache.Unlock()
	defer func() {
		githubStatsCache.Lock()
		githubStatsCache.stars = previousStars
		githubStatsCache.available = previousAvailable
		githubStatsCache.fetchedAt = previousFetchedAt
		githubStatsCache.Unlock()
		githubReleaseCache.Lock()
		githubReleaseCache.latestRelease = previousRelease
		githubReleaseCache.releaseAvailable = previousReleaseAvailable
		githubReleaseCache.releaseFetchedAt = previousReleaseFetchedAt
		githubReleaseCache.Unlock()
	}()

	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/platform", nil)
	(&server{}).platformInfo(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("platform info returned %d", response.Code)
	}
	var payload struct {
		Version         string        `json:"version"`
		GitHubStars     int           `json:"githubStars"`
		UpdateAvailable bool          `json:"updateAvailable"`
		LatestRelease   githubRelease `json:"latestRelease"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Version != version || payload.GitHubStars != 42 || !payload.UpdateAvailable || payload.LatestRelease.Version != "0.6.0" {
		t.Fatalf("unexpected platform info: %#v", payload)
	}
}

func TestNewerVersion(t *testing.T) {
	tests := []struct {
		candidate string
		current   string
		want      bool
	}{
		{candidate: "v0.6.0", current: "0.5.0", want: true},
		{candidate: "0.5.1", current: "0.5.0", want: true},
		{candidate: "1.0.0", current: "0.99.99", want: true},
		{candidate: "0.5.0", current: "0.5.0", want: false},
		{candidate: "0.4.9", current: "0.5.0", want: false},
		{candidate: "latest", current: "0.5.0", want: false},
	}
	for _, test := range tests {
		if got := newerVersion(test.candidate, test.current); got != test.want {
			t.Fatalf("newerVersion(%q, %q) = %t, want %t", test.candidate, test.current, got, test.want)
		}
	}
}
