package commands

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"regexp"
	"strings"
)

// gitPushError carries the captured stderr from a failed git push,
// enabling fine-grained error translation in formatPushError.
type gitPushError struct {
	Stderr string
	cause  error
}

func (e *gitPushError) Error() string { return e.Stderr }
func (e *gitPushError) Unwrap() error { return e.cause }

// runGit runs a git command in dir and returns trimmed stdout.
// Returns "" on any error (including non-zero exit).
func runGit(dir string, args ...string) string {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// runGitCmd runs a git command in dir, discarding stdout.
// On failure, wraps stderr (if non-empty) as the error message.
func runGitCmd(dir string, args ...string) error {
	var stderr bytes.Buffer
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if s := strings.TrimSpace(stderr.String()); s != "" {
			return fmt.Errorf("%s", s)
		}
		return err
	}
	return nil
}

// runGitPush runs "git push bootcraft HEAD:main" in dir.
// Git output (progress lines) is suppressed for a clean CLI UX.
// On failure, returns *gitPushError with the captured stderr.
func runGitPush(dir string) error {
	var stderrBuf bytes.Buffer
	cmd := exec.Command("git", "push", "bootcraft", "HEAD:main")
	cmd.Dir = dir
	cmd.Stdout = io.Discard
	cmd.Stderr = &stderrBuf
	if err := cmd.Run(); err != nil {
		return &gitPushError{Stderr: stderrBuf.String(), cause: err}
	}
	return nil
}

// formatPushError translates a raw git push error into a user-friendly message.
// If err is not a *gitPushError it is returned unchanged.
func formatPushError(err error, course, language string) error {
	var pushErr *gitPushError
	if !errors.As(err, &pushErr) {
		return err
	}
	s := pushErr.Stderr
	switch {
	case strings.Contains(s, "non-fast-forward"):
		return errors.New("❌ 推送被拒绝（non-fast-forward）\n   请运行: git pull bootcraft main --rebase")
	case strings.Contains(s, "rejected"):
		return fmt.Errorf("❌ 推送被拒绝:\n%s", strings.TrimSpace(s))
	case strings.Contains(s, "Authentication failed") ||
		strings.Contains(s, "403") || strings.Contains(s, "401"):
		return errors.New("❌ 认证失败，请重新登录: bootcraft login")
	case strings.Contains(s, "server busy"):
		return errors.New("❌ 服务器繁忙，请稍后重试")
	case strings.Contains(s, "not found") || strings.Contains(s, "仓库不存在"):
		return fmt.Errorf(
			"❌ 推送失败：未找到仓库记录（%s / %s）\n   请先前往 https://www.bootcraft.cn 创建该课程的仓库，再重新提交",
			course, language,
		)
	default:
		if s := strings.TrimSpace(s); s != "" {
			return fmt.Errorf("❌ 推送失败:\n%s", s)
		}
		return fmt.Errorf("❌ 推送失败: %w", err)
	}
}

// gitRootDir returns the git repository root found by walking up from startDir.
func gitRootDir(startDir string) (string, error) {
	var out bytes.Buffer
	cmd := exec.Command("git", "rev-parse", "--show-toplevel")
	cmd.Dir = startDir
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return "", errors.New("当前目录不在 git 仓库中，请在课程目录中运行此命令")
	}
	return strings.TrimSpace(out.String()), nil
}

// parseRepoSlug extracts course and language from a bootcraft remote URL.
//
//	"https://git.bootcraft.cn/tinydsa-java.git" → ("tinydsa", "java")
//
// Uses strings.LastIndex("-") identical to the server's ParseRepoSlug logic,
// so "my-course-go" correctly yields ("my-course", "go").
func parseRepoSlug(remoteURL string) (course, language string, err error) {
	base := remoteURL
	if i := strings.LastIndex(base, "/"); i >= 0 {
		base = base[i+1:]
	}
	base = strings.TrimSuffix(base, ".git")
	i := strings.LastIndex(base, "-")
	if i <= 0 || i == len(base)-1 {
		return "", "", fmt.Errorf("无法从 remote URL 解析 course/language: %q", remoteURL)
	}
	return base[:i], base[i+1:], nil
}

// tokenRe matches the user-info component (user:pass@) in an HTTP(S) URL.
var tokenRe = regexp.MustCompile(`(https?://)([^@]+@)`)

// stripToken removes embedded credentials from a git remote URL.
//
//	"https://x:TOKEN@git.bootcraft.cn/..." → "https://git.bootcraft.cn/..."
func stripToken(rawURL string) string {
	return tokenRe.ReplaceAllString(rawURL, "$1")
}

// setGitRemoteURL runs "git remote set-url <remote> <url>" in dir.
func setGitRemoteURL(dir, remote, rawURL string) error {
	return runGitCmd(dir, "remote", "set-url", remote, rawURL)
}

// ensureBootcraftRemote makes sure the "bootcraft" remote exists in dir with
// the canonical clean URL (no embedded token).  It creates the remote if
// absent, or normalises the URL if it was left with a token from a previous
// interrupted push.
func ensureBootcraftRemote(dir, course, language string) error {
	cleanURL := fmt.Sprintf("https://git.bootcraft.cn/%s-%s.git", course, language)
	existing := runGit(dir, "remote", "get-url", "bootcraft")
	if existing == "" {
		if err := runGitCmd(dir, "remote", "add", "bootcraft", cleanURL); err != nil {
			return fmt.Errorf("添加 bootcraft remote 失败: %w", err)
		}
		return nil
	}
	if stripToken(existing) != cleanURL {
		if err := setGitRemoteURL(dir, "bootcraft", cleanURL); err != nil {
			return fmt.Errorf("更新 bootcraft remote 失败: %w", err)
		}
	}
	return nil
}

// withTokenRemote temporarily embeds the auth token in the "bootcraft" remote
// URL, calls fn(), then restores the clean URL via defer — so the token is
// never stored persistently in .git/config.
func withTokenRemote(dir, token, course, language string, fn func() error) error {
	authURL := fmt.Sprintf("https://x:%s@git.bootcraft.cn/%s-%s.git", token, course, language)
	cleanURL := fmt.Sprintf("https://git.bootcraft.cn/%s-%s.git", course, language)
	if err := setGitRemoteURL(dir, "bootcraft", authURL); err != nil {
		return fmt.Errorf("设置 remote URL 失败: %w", err)
	}
	defer setGitRemoteURL(dir, "bootcraft", cleanURL) //nolint:errcheck
	return fn()
}
