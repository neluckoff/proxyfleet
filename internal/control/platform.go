package control

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
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

var githubReleaseCache struct {
	sync.Mutex
	latestRelease    githubRelease
	releaseAvailable bool
	releaseFetchedAt time.Time
}

type githubRelease struct {
	Version     string    `json:"version"`
	Tag         string    `json:"tag"`
	Name        string    `json:"name"`
	URL         string    `json:"url"`
	PublishedAt time.Time `json:"publishedAt,omitempty"`
}

func (s *server) platformInfo(w http.ResponseWriter, r *http.Request) {
	type starsResult struct {
		stars     int
		available bool
	}
	type releaseResult struct {
		release   githubRelease
		available bool
	}
	starsChannel := make(chan starsResult, 1)
	releaseChannel := make(chan releaseResult, 1)
	go func() {
		stars, available := githubStars(r.Context())
		starsChannel <- starsResult{stars: stars, available: available}
	}()
	go func() {
		release, available := githubLatestRelease(r.Context())
		releaseChannel <- releaseResult{release: release, available: available}
	}()
	starsData := <-starsChannel
	releaseData := <-releaseChannel
	response := map[string]any{
		"version":         version,
		"githubUrl":       githubRepository,
		"updateAvailable": releaseData.available && newerVersion(releaseData.release.Version, version),
	}
	if starsData.available {
		response["githubStars"] = starsData.stars
	}
	if releaseData.available {
		response["latestRelease"] = releaseData.release
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

func githubLatestRelease(parent context.Context) (githubRelease, bool) {
	githubReleaseCache.Lock()
	defer githubReleaseCache.Unlock()
	if time.Since(githubReleaseCache.releaseFetchedAt) < 15*time.Minute {
		return githubReleaseCache.latestRelease, githubReleaseCache.releaseAvailable
	}
	githubReleaseCache.releaseFetchedAt = time.Now()

	ctx, cancel := context.WithTimeout(parent, 3*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.github.com/repos/neluckoff/proxyfleet/releases/latest", nil)
	if err != nil {
		return githubReleaseCache.latestRelease, githubReleaseCache.releaseAvailable
	}
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	request.Header.Set("User-Agent", "ProxyFleet/"+version)
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return githubReleaseCache.latestRelease, githubReleaseCache.releaseAvailable
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return githubReleaseCache.latestRelease, githubReleaseCache.releaseAvailable
	}
	var payload struct {
		Tag         string    `json:"tag_name"`
		Name        string    `json:"name"`
		URL         string    `json:"html_url"`
		PublishedAt time.Time `json:"published_at"`
	}
	if json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&payload) != nil {
		return githubReleaseCache.latestRelease, githubReleaseCache.releaseAvailable
	}
	releaseVersion, ok := parseVersion(payload.Tag)
	if !ok || payload.URL == "" {
		return githubReleaseCache.latestRelease, githubReleaseCache.releaseAvailable
	}
	githubReleaseCache.latestRelease = githubRelease{
		Version:     releaseVersion,
		Tag:         payload.Tag,
		Name:        payload.Name,
		URL:         payload.URL,
		PublishedAt: payload.PublishedAt,
	}
	githubReleaseCache.releaseAvailable = true
	return githubReleaseCache.latestRelease, true
}

func newerVersion(candidate, current string) bool {
	candidateVersion, candidateOK := parseVersion(candidate)
	currentVersion, currentOK := parseVersion(current)
	if !candidateOK || !currentOK {
		return false
	}
	candidateParts := versionParts(candidateVersion)
	currentParts := versionParts(currentVersion)
	for index := range candidateParts {
		if candidateParts[index] != currentParts[index] {
			return candidateParts[index] > currentParts[index]
		}
	}
	return false
}

func parseVersion(value string) (string, bool) {
	value = strings.TrimSpace(strings.TrimPrefix(strings.ToLower(value), "v"))
	if separator := strings.IndexAny(value, "-+"); separator >= 0 {
		value = value[:separator]
	}
	parts := strings.Split(value, ".")
	if len(parts) != 3 {
		return "", false
	}
	for _, part := range parts {
		if part == "" {
			return "", false
		}
		if _, err := strconv.Atoi(part); err != nil {
			return "", false
		}
	}
	return strings.Join(parts, "."), true
}

func versionParts(value string) [3]int {
	var result [3]int
	for index, part := range strings.Split(value, ".") {
		result[index], _ = strconv.Atoi(part)
	}
	return result
}
