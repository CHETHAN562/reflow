package update

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"reflow/internal/util"
	"strings"
	"sync"
	"time"

	hversion "github.com/hashicorp/go-version"
)

const (
	githubAPIBase   = "https://api.github.com"
	defaultInterval = 24 * time.Hour
	CacheFileName   = ".update_cache.json"
)

// Cache stores information about the last update check.
type Cache struct {
	LastCheckTime      time.Time `json:"last_check_time"`
	LatestVersionFound string    `json:"latest_version_found"`
	ReleaseURL         string    `json:"release_url"`
}

// CheckResult holds the outcome of an update check.
type CheckResult struct {
	LatestVersion string
	ReleaseURL    string
	IsNewer       bool
	Error         error
}

var (
	checkMutex     sync.Mutex
	lastResult     *CheckResult
	lastResultTime time.Time
)

// readCache loads the cache file.
func readCache(cacheFilePath string) (*Cache, error) {
	data, err := os.ReadFile(cacheFilePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to read update cache file %s: %w", cacheFilePath, err)
	}
	if len(data) == 0 {
		return nil, nil
	}

	var cache Cache
	if err := json.Unmarshal(data, &cache); err != nil {
		util.Log.Warnf("Failed to parse update cache file %s, ignoring: %v", cacheFilePath, err)
		return nil, nil
	}
	return &cache, nil
}

// writeCache saves the cache file.
func writeCache(cacheFilePath string, cache *Cache) error {
	data, err := json.MarshalIndent(cache, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal update cache: %w", err)
	}
	cacheDir := filepath.Dir(cacheFilePath)
	if err := os.MkdirAll(cacheDir, 0750); err != nil {
		return fmt.Errorf("failed to create dir for update cache '%s': %w", cacheDir, err)
	}
	if err := os.WriteFile(cacheFilePath, data, 0640); err != nil {
		return fmt.Errorf("failed to write update cache file %s: %w", cacheFilePath, err)
	}
	return nil
}

// fetchLatestRelease queries the GitHub API for the latest release.
func fetchLatestRelease(ctx context.Context, repo string) (string, string, error) {
	url := fmt.Sprintf("%s/repos/%s/releases/latest", githubAPIBase, repo)
	util.Log.Debugf("Fetching latest release from %s", url)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return "", "", fmt.Errorf("failed to create request to GitHub API: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github.v3+json")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", "", fmt.Errorf("failed to fetch latest release from GitHub: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		if resp.StatusCode == http.StatusNotFound {
			return "", "", fmt.Errorf("repository %s or its releases not found (status: %d)", repo, resp.StatusCode)
		}
		if resp.StatusCode == http.StatusForbidden {
			util.Log.Warnf("GitHub API rate limit likely exceeded or token required for %s", repo)
		}
		return "", "", fmt.Errorf("failed to fetch latest release from GitHub (status: %d): %s", resp.StatusCode, string(bodyBytes))
	}

	var releaseInfo struct {
		TagName string `json:"tag_name"`
		HTMLURL string `json:"html_url"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&releaseInfo); err != nil {
		return "", "", fmt.Errorf("failed to parse GitHub release response: %w", err)
	}

	if releaseInfo.TagName == "" {
		return "", "", fmt.Errorf("latest GitHub release does not have a tag_name")
	}

	util.Log.Debugf("Latest GitHub release found: Tag=%s, URL=%s", releaseInfo.TagName, releaseInfo.HTMLURL)
	return releaseInfo.TagName, releaseInfo.HTMLURL, nil
}

// compareVersions returns true if latestVersionStr is newer than currentVersionStr.
func compareVersions(currentVersionStr, latestVersionStr string) (bool, error) {
	currentV, err := hversion.NewVersion(strings.TrimPrefix(currentVersionStr, "v"))
	if err != nil {
		return false, fmt.Errorf("invalid current version format '%s': %w", currentVersionStr, err)
	}

	latestV, err := hversion.NewVersion(strings.TrimPrefix(latestVersionStr, "v"))
	if err != nil {
		return false, fmt.Errorf("invalid latest version format '%s': %w", latestVersionStr, err)
	}

	return currentV.LessThan(latestV), nil
}

// CheckForUpdate checks GitHub releases for a newer version, using caching.
// Returns the latest version found, its URL, whether it's newer, and any error during the check process.
func CheckForUpdate(currentVersionStr string, repo string, cacheFilePath string, checkInterval time.Duration) (*CheckResult, error) {
	checkMutex.Lock()
	defer checkMutex.Unlock()

	if lastResult != nil && time.Since(lastResultTime) < 1*time.Minute {
		util.Log.Debug("Update check recently performed, using in-memory result.")
		return lastResult, nil
	}

	// --- Input Validation ---
	if checkInterval <= 0 {
		checkInterval = defaultInterval
	}
	if currentVersionStr == "" || currentVersionStr == "dev" {
		util.Log.Debug("Running development version or version unknown, skipping update check.")
		return &CheckResult{Error: fmt.Errorf("development version")}, nil
	}
	if repo == "" {
		return &CheckResult{Error: fmt.Errorf("repository not specified")}, fmt.Errorf("repository slug cannot be empty for update check")
	}

	// --- Cache Check ---
	cache, err := readCache(cacheFilePath)
	if err != nil {
		util.Log.Warnf("Could not read update cache: %v", err)
	}

	if cache != nil && time.Since(cache.LastCheckTime) < checkInterval {
		util.Log.Debugf("Update check cache is fresh (checked at %s). Using cached version: %s", cache.LastCheckTime.Format(time.RFC3339), cache.LatestVersionFound)
		isNewer, compErr := compareVersions(currentVersionStr, cache.LatestVersionFound)
		if compErr != nil {
			util.Log.Warnf("Failed to compare current version with cached version: %v", compErr)
		}
		result := &CheckResult{LatestVersion: cache.LatestVersionFound, ReleaseURL: cache.ReleaseURL, IsNewer: isNewer, Error: compErr}
		lastResult = result
		lastResultTime = time.Now()
		return result, nil
	}

	// --- GitHub API Check ---
	util.Log.Debug("Update check cache stale or missing, checking GitHub API...")
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	latestVersionTag, releaseURL, fetchErr := fetchLatestRelease(ctx, repo)
	result := &CheckResult{LatestVersion: latestVersionTag, ReleaseURL: releaseURL}

	if fetchErr != nil {
		util.Log.Warnf("Failed to fetch latest release from GitHub: %v", fetchErr)
		result.Error = fetchErr
		return result, fetchErr
	}

	// --- Write Cache ---
	newCache := &Cache{LastCheckTime: time.Now(), LatestVersionFound: latestVersionTag, ReleaseURL: releaseURL}
	if writeErr := writeCache(cacheFilePath, newCache); writeErr != nil {
		util.Log.Warnf("Failed to write update cache: %v", writeErr)
	}

	// --- Compare Versions ---
	isNewer, compErr := compareVersions(currentVersionStr, latestVersionTag)
	if compErr != nil {
		util.Log.Warnf("Failed to compare versions ('%s' vs '%s'): %v", currentVersionStr, latestVersionTag, compErr)
		result.Error = compErr
		isNewer = false
	}
	result.IsNewer = isNewer

	lastResult = result
	lastResultTime = time.Now()
	return result, nil
}
