package commands

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"time"
)

type testerMeta struct {
	Version  string    `json:"version"`
	CachedAt time.Time `json:"cached_at"`
}

type ghRelease struct {
	TagName string `json:"tag_name"`
}

const testerCacheTTL = 24 * time.Hour

// ensureTester ensures the tester binary for the given course is cached locally
// and up-to-date. Returns the absolute path to the binary.
//
// Cache layout:
//
//	~/.tinycs/testers/<course>/tester        (binary)
//	~/.tinycs/testers/<course>/meta.json     (version + cached_at)
func ensureTester(course string) (string, error) {
	home, _ := os.UserHomeDir()
	cacheDir := filepath.Join(home, ".tinycs", "testers", course)
	metaPath := filepath.Join(cacheDir, "meta.json")
	testerPath := filepath.Join(cacheDir, "tester")
	if runtime.GOOS == "windows" {
		testerPath += ".exe"
	}

	// 1. Read local meta.
	var meta testerMeta
	if data, err := os.ReadFile(metaPath); err == nil {
		_ = json.Unmarshal(data, &meta)
	}

	// 2. If within TTL and binary exists, return immediately.
	if meta.Version != "" && time.Since(meta.CachedAt) < testerCacheTTL {
		if _, err := os.Stat(testerPath); err == nil {
			return testerPath, nil
		}
	}

	// 3. Query GitHub Releases for the latest tag.
	latest, err := fetchLatestTesterRelease(course)
	if err != nil {
		// Graceful degradation: if we already have a binary, use it.
		if meta.Version != "" {
			if _, serr := os.Stat(testerPath); serr == nil {
				fmt.Printf("⚠️  无法查询最新版本，使用缓存版本 %s\n", meta.Version)
				return testerPath, nil
			}
		}
		return "", fmt.Errorf("查询 %s-tester 最新版本失败: %w", course, err)
	}

	// 4. If already on the latest version, refresh cached_at and return.
	if meta.Version == latest {
		if _, err := os.Stat(testerPath); err == nil {
			meta.CachedAt = time.Now()
			_ = saveTesterMeta(metaPath, meta)
			return testerPath, nil
		}
	}

	// 5. Download the new binary.
	platform := testerPlatform()
	fname := fmt.Sprintf("%s-tester-%s", course, platform)
	dlURL := fmt.Sprintf(
		"https://github.com/tinycs-cn/%s-tester/releases/download/%s/%s",
		course, latest, fname,
	)

	fmt.Printf("📦 下载 %s-tester %s ...\n", course, latest)
	if err := downloadTesterBinary(dlURL, testerPath); err != nil {
		return "", fmt.Errorf("下载 %s-tester 失败: %w", course, err)
	}

	meta.Version = latest
	meta.CachedAt = time.Now()
	_ = saveTesterMeta(metaPath, meta)

	return testerPath, nil
}

func fetchLatestTesterRelease(course string) (string, error) {
	url := fmt.Sprintf(
		"https://api.github.com/repos/tinycs-cn/%s-tester/releases/latest",
		course,
	)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("GitHub API 返回 %d", resp.StatusCode)
	}

	var rel ghRelease
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return "", err
	}
	if rel.TagName == "" {
		return "", fmt.Errorf("未找到发布版本")
	}
	return rel.TagName, nil
}

func testerPlatform() string {
	goos := runtime.GOOS
	goarch := runtime.GOARCH
	if goos == "windows" {
		return "windows-amd64.exe"
	}
	return fmt.Sprintf("%s-%s", goos, goarch)
}

func downloadTesterBinary(url, dest string) error {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, url)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(dest), 0750); err != nil {
		return err
	}
	// Write with restricted perms first, then set executable.
	if err := os.WriteFile(dest, data, 0600); err != nil {
		return err
	}
	return os.Chmod(dest, 0755) //nolint:gosec — intentional executable bit
}

func saveTesterMeta(path string, meta testerMeta) error {
	data, err := json.Marshal(meta)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0600)
}
