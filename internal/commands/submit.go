package commands

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/bootcraft-cn/cli/internal/client"
	"github.com/bootcraft-cn/cli/internal/config"
	"github.com/bootcraft-cn/cli/internal/ui"

	"gopkg.in/yaml.v3"
)

type bootcraftMeta struct {
	Course   string `yaml:"course"`
	Language string `yaml:"language"`
}

// trackURLPath maps API track values to web URL path segments.
// Mirrors bootcraft-web-cn/apps/main/src/config/tracks.ts.
func trackURLPath(track string) string {
	switch track {
	case "system":
		return "systems"
	default:
		return track
	}
}

// SubmitCommand implements "bootcraft submit".
//
// v2 flow (git push based):
//  1. Load auth token from config
//  2. Resolve course + language + project dir (remote URL → bootcraft.yml fallback)
//  3. Ensure "bootcraft" remote exists with clean URL
//  4. Build commit message (injecting [stage=xxx] if --stage given)
//  5. git add -A && git commit --allow-empty
//  6. git push bootcraft HEAD:main  (token embedded temporarily, restored by defer)
//  7. Poll GET /v1/cli/submissions/by-commit (6×500ms) to get submission ID
//  8. Stream eval logs via SSE → render final result
func SubmitCommand(args []string) error {
	flags := flag.NewFlagSet("submit", flag.ContinueOnError)
	stage := flags.String("stage", "", "指定评测关卡 (slug)")
	message := flags.String("message", "", "自定义 commit message（默认：\"bootcraft submit\"）")
	dryRun := flags.Bool("dry-run", false, "打印将执行的 git 命令，不实际推送")
	apiURL := flags.String("api-url", "", "API 地址（内部测试用）")
	if err := flags.Parse(args); err != nil {
		return err
	}

	// 1. Auth
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("加载配置失败: %w", err)
	}
	token := cfg.GetToken()
	if token == "" {
		return errors.New("未登录，请先运行: bootcraft login")
	}

	// 2. Resolve course + language + project dir
	course, language, projectDir, err := resolveProject()
	if err != nil {
		return err
	}

	// 3. Ensure bootcraft remote exists (clean URL, no token)
	if err := ensureBootcraftRemote(projectDir, course, language); err != nil {
		return err
	}

	// 4. Build commit message
	commitMsg := buildCommitMessage(*message, *stage)

	if *dryRun {
		ui.Println("[dry-run] 将执行以下操作：")
		ui.Printf("  git -C %q add -A\n", projectDir)
		ui.Printf("  git -C %q commit --allow-empty -m %q\n", projectDir, commitMsg)
		ui.Printf("  git -C %q push bootcraft HEAD:main\n", projectDir)
		return nil
	}

	// 5. Auto-commit
	ui.Print("📝 自动 commit 中...")
	if err := runGitCmd(projectDir, "add", "-A"); err != nil {
		return fmt.Errorf("git add 失败: %w", err)
	}
	if err := runGitCmd(projectDir, "commit", "--allow-empty", "-m", commitMsg); err != nil {
		return fmt.Errorf("git commit 失败: %w", err)
	}
	commitSHA := runGit(projectDir, "rev-parse", "HEAD")
	ui.Printf(" (commit: %.7s)\n", commitSHA)

	// 6. git push (token embedded in remote URL, restored by withTokenRemote's defer)
	ui.Println("🚀 推送到 Bootcraft...")
	pushErr := withTokenRemote(projectDir, token, course, language, func() error {
		return runGitPush(projectDir)
	})
	if pushErr != nil {
		return formatPushError(pushErr, course, language)
	}

	// 7. Poll by-commit to discover submission ID
	baseURL := cfg.GetAPIURL(*apiURL)
	c := client.New(baseURL, token)
	byCommit, err := pollByCommit(c, commitSHA, course, language)
	if err != nil {
		return err
	}
	ui.Printf("📋 Stage: %s「%s」\n", byCommit.StageSlug, byCommit.StageName)
	ui.Printf("🆔 Submission: %s\n", byCommit.SubmissionID)

	// 8. Stream eval logs + render result
	result, skipLogs, err := watchSubmission(c, byCommit.SubmissionID)
	if err != nil {
		return err
	}
	return renderResult(result, skipLogs)
}

// resolveProject returns course, language, and the git repo root directory.
//
// Resolution order:
//  1. "bootcraft" remote URL (daily path — no files needed)
//  2. bootcraft.yml walking up from cwd (first-time setup path)
//
// The returned projectDir is always the git repo root (from git rev-parse
// --show-toplevel), which is the correct base for all git sub-commands.
func resolveProject() (course, language, projectDir string, err error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", "", "", fmt.Errorf("获取当前目录失败: %w", err)
	}

	projectDir, err = gitRootDir(cwd)
	if err != nil {
		return "", "", "", err
	}

	// ① Remote URL (covers 99% of daily submits once remote is configured)
	remoteURL := runGit(projectDir, "remote", "get-url", "bootcraft")
	if remoteURL != "" {
		c, l, rerr := parseRepoSlug(remoteURL)
		if rerr == nil {
			return c, l, projectDir, nil
		}
		// URL exists but not a recognisable bootcraft slug — fall through to yml
	}

	// ② bootcraft.yml — walk up from cwd (mirrors v1 behaviour for nested projects)
	dir := cwd
	for {
		configPath := filepath.Join(dir, "bootcraft.yml")
		data, ferr := os.ReadFile(configPath)
		if ferr == nil {
			var meta bootcraftMeta
			if yerr := yaml.Unmarshal(data, &meta); yerr == nil &&
				meta.Course != "" && meta.Language != "" {
				return meta.Course, meta.Language, projectDir, nil
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}

	return "", "", "", errors.New(
		"找不到 Bootcraft remote，请在课程目录中运行，\n" +
			"或先到 https://www.bootcraft.cn 创建仓库后克隆模版",
	)
}

// buildCommitMessage constructs the git commit message for a submit.
//
//	No flags          → "bootcraft submit"
//	--stage s03       → "bootcraft submit [stage=s03]"
//	--message "msg"   → "msg"
//	--message + stage → "msg [stage=s03]"
func buildCommitMessage(customMsg, stageSlug string) string {
	base := "bootcraft submit"
	if customMsg != "" {
		base = customMsg
	}
	if stageSlug != "" {
		return base + " [stage=" + stageSlug + "]"
	}
	return base
}

// pollByCommit polls GET /v1/cli/submissions/by-commit until the submission
// created by the recent git push is visible, or timeout is reached.
//
// Strategy: 6 attempts × 500ms = ~3s total.  HandlePush runs synchronously
// post-ack on the server side (~50–100ms), so the first poll usually hits.
func pollByCommit(c *client.Client, commitSHA, courseSlug, languageSlug string) (*client.ByCommitResponse, error) {
	spinner := ui.NewSpinner("等待提交创建")
	defer spinner.Stop()

	const maxRetries = 6
	const interval = 500 * time.Millisecond

	for i := 0; i < maxRetries; i++ {
		if i > 0 {
			time.Sleep(interval)
		}
		resp, err := c.GetSubmissionByCommit(commitSHA, courseSlug, languageSlug)
		if err == nil {
			return resp, nil
		}
		var apiErr *client.APIError
		if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusNotFound {
			continue // submission not yet written to DB — retry
		}
		return nil, fmt.Errorf("查询提交状态失败: %w", err)
	}
	return nil, errors.New("等待提交创建超时，建议前往网页查看结果")
}

// --- evaluation log streaming (unchanged from v1) ---

func watchSubmission(c *client.Client, submissionID string) (*client.SubmissionStatusResponse, bool, error) {
	tokenResp, err := c.GetTriggerToken(submissionID)
	if err != nil {
		ui.Warn("实时日志不可用，切换到轮询模式")
		result, err := pollSubmission(c, submissionID)
		return result, false, err
	}

	result, streamErr := streamEvalLogs(c, submissionID, tokenResp.StreamURL, tokenResp.PublicAccessToken)
	if streamErr != nil {
		ui.Warn(fmt.Sprintf("SSE 连接中断，切换到轮询模式 (err: %v)", streamErr))
		result, err := pollSubmission(c, submissionID)
		return result, false, err
	}
	return result, true, nil
}

func streamEvalLogs(c *client.Client, submissionID, streamURL, accessToken string) (*client.SubmissionStatusResponse, error) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var finalResult *client.SubmissionStatusResponse
	go func() {
		deadline := time.Now().Add(120 * time.Second)
		for time.Now().Before(deadline) {
			time.Sleep(3 * time.Second)
			status, err := c.GetSubmissionStatus(submissionID)
			if err != nil {
				continue
			}
			if client.IsTerminalStatus(status.Status) {
				finalResult = status
				time.Sleep(3 * time.Second)
				cancel()
				return
			}
		}
		cancel()
	}()

	sseURL := streamURL
	if sseURL == "" {
		return nil, fmt.Errorf("stream URL not provided by server")
	}
	req, err := http.NewRequestWithContext(ctx, "GET", sseURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Cache-Control", "no-cache")

	sseClient := &http.Client{Timeout: 5 * time.Minute}
	resp, err := sseClient.Do(req)
	if err != nil && ctx.Err() == nil {
		return nil, err
	}
	if resp != nil {
		defer resp.Body.Close()
		if resp.StatusCode != 200 {
			return nil, fmt.Errorf("SSE status %d (url=%s)", resp.StatusCode, streamURL)
		}
		spinner := ui.NewSpinner("评测中")
		firstLine := true
		scanner := bufio.NewScanner(resp.Body)
		for scanner.Scan() {
			line := scanner.Text()
			if strings.HasPrefix(line, "data: ") {
				if firstLine {
					spinner.Stop()
					firstLine = false
				}
				chunk := strings.TrimPrefix(line, "data: ")
				if chunk == "closed" || chunk == "[DONE]" {
					break
				}
				var text string
				if jsonErr := json.Unmarshal([]byte(chunk), &text); jsonErr == nil {
					if text != "" {
						fmt.Println(text)
					}
				} else if chunk != "" {
					fmt.Println(chunk)
				}
			}
		}
		if firstLine {
			spinner.Stop()
		}
	}

	if finalResult != nil {
		return finalResult, nil
	}
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		status, err := c.GetSubmissionStatus(submissionID)
		if err != nil {
			return nil, err
		}
		if client.IsTerminalStatus(status.Status) {
			return status, nil
		}
		time.Sleep(1 * time.Second)
	}
	return c.GetSubmissionStatus(submissionID)
}

func pollSubmission(c *client.Client, submissionID string) (*client.SubmissionStatusResponse, error) {
	spinner := ui.NewSpinner("评测中")
	defer spinner.Stop()
	deadline := time.Now().Add(120 * time.Second)
	for time.Now().Before(deadline) {
		status, err := c.GetSubmissionStatus(submissionID)
		if err != nil {
			return nil, fmt.Errorf("查询状态失败: %w", err)
		}
		if client.IsTerminalStatus(status.Status) {
			return status, nil
		}
		time.Sleep(2 * time.Second)
	}
	return nil, errors.New("评测超时，请稍后在网页查看结果")
}

func renderResult(result *client.SubmissionStatusResponse, skipLogs bool) error {
	fmt.Println()
	durationStr := ""
	if result.DurationMs != nil {
		durationStr = fmt.Sprintf(" (%.1fs)", float64(*result.DurationMs)/1000)
	}

	switch result.Status {
	case "success":
		ui.Success(fmt.Sprintf("✅ %s「%s」通过！%s", result.StageSlug, result.StageName, durationStr))
		if result.StagePosition > 0 && result.CourseSlug != "" && result.CourseTrack != "" && result.Language != "" {
			fmt.Println()
			url := fmt.Sprintf("https://www.bootcraft.cn/courses/%s/%s/repos/%s/stages/%d",
				trackURLPath(result.CourseTrack), result.CourseSlug, result.Language, result.StagePosition)
			ui.Info(fmt.Sprintf("👉 前往网页点击「完成本关」解锁下一关：%s", url))
		}
		return nil
	case "failure":
		ui.Error(fmt.Sprintf("❌ %s「%s」未通过%s", result.StageSlug, result.StageName, durationStr))
		if !skipLogs && result.Logs != "" {
			fmt.Println()
			fmt.Println(result.Logs)
		}
		return errors.New("评测未通过")
	case "error":
		if strings.Contains(result.Logs, "EVAL_SYSTEM_BUSY") {
			ui.Warn("⏳ 评测系统繁忙，请30秒后再试")
			return errors.New("评测系统繁忙，请30秒后再试")
		}
		ui.Error(fmt.Sprintf("💥 评测出错%s", durationStr))
		if !skipLogs && result.Logs != "" {
			fmt.Println()
			fmt.Println(result.Logs)
		}
		return errors.New("评测出错")
	case "timeout":
		ui.Warn("⏰ 评测超时，请稍后在网页查看结果")
		return errors.New("评测超时")
	default:
		return fmt.Errorf("未知评测状态: %s", result.Status)
	}
}
