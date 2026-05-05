package commands

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
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
	case strings.Contains(s, "尚未解锁"):
		// Extract and surface the server's message directly.
		// e.g. "ng 该关卡尚未解锁，请先完成前面的关卡"
		if i := strings.Index(s, "ng "); i >= 0 {
			msg := strings.TrimSpace(s[i+3:])
			if nl := strings.IndexByte(msg, '\n'); nl >= 0 {
				msg = msg[:nl]
			}
			return fmt.Errorf("❌ %s\n   请先在网页点击「完成本关」解锁下一关", msg)
		}
		return errors.New("❌ 该关卡尚未解锁，请先在网页点击「完成本关」解锁下一关")
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

// suspiciousDirPrefixes lists directory names that should never be committed.
// A staged path is flagged when its first path component matches one of these.
var suspiciousDirPrefixes = []string{
	"node_modules/",
	"__pycache__/",
	".venv/",
	"venv/",
	"target/",
	"vendor/",
	"dist/",
	".next/",
	".nuxt/",
	"build/",
	".gradle/",
}

// suspiciousExts lists file extensions that almost always indicate build
// artefacts or compiled binaries — not source files meant for evaluation.
var suspiciousExts = []string{
	".pyc", ".pyo",
	".class", ".jar", ".war", ".ear",
	".o", ".a", ".so", ".exe",
}

// classifyStagedPaths returns the subset of paths that look like build
// artefacts, dependency trees, or compiled binaries.  Extracted as a pure
// function so it can be unit-tested without a real git repo.
func classifyStagedPaths(paths []string) []string {
	var flagged []string
	for _, path := range paths {
		if path == "" {
			continue
		}
		pathSlash := filepath.ToSlash(path)

		for _, prefix := range suspiciousDirPrefixes {
			if strings.HasPrefix(pathSlash, prefix) || strings.Contains(pathSlash, "/"+prefix) {
				flagged = append(flagged, path)
				goto next
			}
		}
		for _, ext := range suspiciousExts {
			if strings.HasSuffix(strings.ToLower(pathSlash), ext) {
				flagged = append(flagged, path)
				goto next
			}
		}
	next:
	}
	return flagged
}

// maxStagedSizeBytes is the client-side total staged file size limit.
// Set to 20 MB — 2× the server's 10 MB packfile limit — to account for git
// delta compression (packfiles are typically 30-70% smaller than raw files).
// Oversized pushes are caught here with a friendly message before any data
// is transferred; the server enforces the hard 10 MB packfile limit as backup.
const maxStagedSizeBytes int64 = 20 << 20 // 20 MB

// checkStagedFiles inspects the git index after "git add -A" and returns an
// error if it finds files that look like build artefacts, dependency trees,
// binaries, or if the total staged size exceeds the client-side limit.
// This mirrors the safety net the old tar.gz Pack() provided.
func checkStagedFiles(dir string) error {
	out := runGit(dir, "diff", "--cached", "--name-only")
	if out == "" {
		return nil
	}

	paths := strings.Split(out, "\n")

	flagged := classifyStagedPaths(paths)

	// Size check: sum working-tree sizes of staged files.
	// Uses os.Stat so it runs without extra git calls.
	var totalSize int64
	for _, p := range paths {
		if p == "" {
			continue
		}
		if info, err := os.Stat(filepath.Join(dir, filepath.FromSlash(p))); err == nil {
			totalSize += info.Size()
		}
	}

	if len(flagged) == 0 && totalSize <= maxStagedSizeBytes {
		return nil
	}

	var sb strings.Builder

	if len(flagged) > 0 {
		const maxShow = 5
		shown := flagged
		extra := 0
		if len(shown) > maxShow {
			extra = len(shown) - maxShow
			shown = shown[:maxShow]
		}
		sb.WriteString("❌ 检测到不应提交的文件（二进制/依赖包/编译产物）：\n")
		for _, f := range shown {
			sb.WriteString("   " + f + "\n")
		}
		if extra > 0 {
			sb.WriteString(fmt.Sprintf("   ... 还有 %d 个文件\n", extra))
		}
		sb.WriteString("\n请将这些路径添加到 .gitignore 后重新运行 bootcraft submit\n")
		sb.WriteString("例如：echo 'node_modules/' >> .gitignore")
	}

	if totalSize > maxStagedSizeBytes {
		if sb.Len() > 0 {
			sb.WriteString("\n\n")
		}
		sb.WriteString(fmt.Sprintf(
			"❌ 提交文件总大小 %.1f MB 超过限制（%.0f MB）\n",
			float64(totalSize)/(1<<20), float64(maxStagedSizeBytes)/(1<<20),
		))
		sb.WriteString("请检查是否有大文件未加入 .gitignore")
	}

	return errors.New(sb.String())
}
