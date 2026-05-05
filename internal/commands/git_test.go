package commands

import (
	"errors"
	"strings"
	"testing"
)

// --- parseRepoSlug ---

func TestParseRepoSlug_Standard(t *testing.T) {
	c, l, err := parseRepoSlug("https://git.bootcraft.cn/tinydsa-java.git")
	if err != nil || c != "tinydsa" || l != "java" {
		t.Fatalf("got (%q, %q, %v)", c, l, err)
	}
}

func TestParseRepoSlug_WithToken(t *testing.T) {
	c, l, err := parseRepoSlug("https://x:TOKEN@git.bootcraft.cn/leetml-python.git")
	if err != nil || c != "leetml" || l != "python" {
		t.Fatalf("got (%q, %q, %v)", c, l, err)
	}
}

func TestParseRepoSlug_MultiHyphenCourse(t *testing.T) {
	// course "my-course" + language "go" — last hyphen is the separator
	c, l, err := parseRepoSlug("https://git.bootcraft.cn/my-course-go.git")
	if err != nil || c != "my-course" || l != "go" {
		t.Fatalf("got (%q, %q, %v)", c, l, err)
	}
}

func TestParseRepoSlug_NoExtension(t *testing.T) {
	// .git suffix is optional in practice
	c, l, err := parseRepoSlug("https://git.bootcraft.cn/tinydsa-java")
	if err != nil || c != "tinydsa" || l != "java" {
		t.Fatalf("got (%q, %q, %v)", c, l, err)
	}
}

func TestParseRepoSlug_NoDash(t *testing.T) {
	_, _, err := parseRepoSlug("https://git.bootcraft.cn/nodash.git")
	if err == nil {
		t.Fatal("expected error for URL with no hyphen in repo slug")
	}
}

func TestParseRepoSlug_TrailingDash(t *testing.T) {
	_, _, err := parseRepoSlug("https://git.bootcraft.cn/trailing-.git")
	if err == nil {
		t.Fatal("expected error for URL ending in hyphen (empty language)")
	}
}

// --- stripToken ---

func TestStripToken_WithToken(t *testing.T) {
	got := stripToken("https://x:mytoken@git.bootcraft.cn/tinydsa-java.git")
	want := "https://git.bootcraft.cn/tinydsa-java.git"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestStripToken_WithoutToken(t *testing.T) {
	url := "https://git.bootcraft.cn/tinydsa-java.git"
	if got := stripToken(url); got != url {
		t.Fatalf("stripToken changed a URL with no credentials: got %q", got)
	}
}

// --- buildCommitMessage ---

func TestBuildCommitMessage_Default(t *testing.T) {
	if got := buildCommitMessage("", ""); got != "bootcraft submit" {
		t.Fatalf("got %q", got)
	}
}

func TestBuildCommitMessage_WithStage(t *testing.T) {
	if got := buildCommitMessage("", "s03"); got != "bootcraft submit [stage=s03]" {
		t.Fatalf("got %q", got)
	}
}

func TestBuildCommitMessage_WithMessage(t *testing.T) {
	if got := buildCommitMessage("fix softmax", ""); got != "fix softmax" {
		t.Fatalf("got %q", got)
	}
}

func TestBuildCommitMessage_WithMessageAndStage(t *testing.T) {
	if got := buildCommitMessage("fix softmax", "s03"); got != "fix softmax [stage=s03]" {
		t.Fatalf("got %q", got)
	}
}

// --- formatPushError ---

func TestFormatPushError_NonFastForward(t *testing.T) {
	err := formatPushError(&gitPushError{Stderr: " ! [rejected] main -> main (non-fast-forward)"}, "tinydsa", "java")
	if err == nil {
		t.Fatal("expected non-nil error")
	}
	msg := err.Error()
	if !strings.Contains(msg, "non-fast-forward") || !strings.Contains(msg, "rebase") {
		t.Fatalf("expected non-fast-forward + rebase hint, got: %q", msg)
	}
}

func TestFormatPushError_AuthFailed(t *testing.T) {
	err := formatPushError(&gitPushError{Stderr: "fatal: Authentication failed for 'https://git.bootcraft.cn/'"}, "tinydsa", "java")
	if err == nil {
		t.Fatal("expected non-nil error")
	}
	if !strings.Contains(err.Error(), "认证失败") {
		t.Fatalf("expected 认证失败, got: %q", err.Error())
	}
}

func TestFormatPushError_RepoNotFound(t *testing.T) {
	err := formatPushError(&gitPushError{Stderr: "remote: 仓库不存在，请先在网站上选择语言并开始课程"}, "tinydsa", "java")
	if err == nil {
		t.Fatal("expected non-nil error")
	}
	if !strings.Contains(err.Error(), "未找到仓库记录") {
		t.Fatalf("expected 未找到仓库记录, got: %q", err.Error())
	}
}

func TestFormatPushError_ServerBusy(t *testing.T) {
	err := formatPushError(&gitPushError{Stderr: "fatal: remote error: server busy, please retry"}, "tinydsa", "java")
	if err == nil {
		t.Fatal("expected non-nil error")
	}
	if !strings.Contains(err.Error(), "繁忙") {
		t.Fatalf("expected 繁忙, got: %q", err.Error())
	}
}

func TestFormatPushError_GenericError(t *testing.T) {
	// A non-gitPushError should be returned unchanged
	origErr := errors.New("some other error")
	if got := formatPushError(origErr, "tinydsa", "java"); got != origErr {
		t.Fatalf("expected same error pointer, got %v", got)
	}
}
