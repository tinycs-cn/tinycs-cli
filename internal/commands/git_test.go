package commands

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// --- parseRepoSlug ---

func TestParseRepoSlug_Standard(t *testing.T) {
	c, l, err := parseRepoSlug("https://git.tinycs.cn/tinydsa-java.git")
	if err != nil || c != "tinydsa" || l != "java" {
		t.Fatalf("got (%q, %q, %v)", c, l, err)
	}
}

func TestParseRepoSlug_WithToken(t *testing.T) {
	c, l, err := parseRepoSlug("https://x:TOKEN@git.tinycs.cn/leetml-python.git")
	if err != nil || c != "leetml" || l != "python" {
		t.Fatalf("got (%q, %q, %v)", c, l, err)
	}
}

func TestParseRepoSlug_MultiHyphenCourse(t *testing.T) {
	// course "my-course" + language "go" — last hyphen is the separator
	c, l, err := parseRepoSlug("https://git.tinycs.cn/my-course-go.git")
	if err != nil || c != "my-course" || l != "go" {
		t.Fatalf("got (%q, %q, %v)", c, l, err)
	}
}

func TestParseRepoSlug_NoExtension(t *testing.T) {
	// .git suffix is optional in practice
	c, l, err := parseRepoSlug("https://git.tinycs.cn/tinydsa-java")
	if err != nil || c != "tinydsa" || l != "java" {
		t.Fatalf("got (%q, %q, %v)", c, l, err)
	}
}

func TestParseRepoSlug_NoDash(t *testing.T) {
	_, _, err := parseRepoSlug("https://git.tinycs.cn/nodash.git")
	if err == nil {
		t.Fatal("expected error for URL with no hyphen in repo slug")
	}
}

func TestParseRepoSlug_TrailingDash(t *testing.T) {
	_, _, err := parseRepoSlug("https://git.tinycs.cn/trailing-.git")
	if err == nil {
		t.Fatal("expected error for URL ending in hyphen (empty language)")
	}
}

// --- stripToken ---

func TestStripToken_WithToken(t *testing.T) {
	got := stripToken("https://x:mytoken@git.tinycs.cn/tinydsa-java.git")
	want := "https://git.tinycs.cn/tinydsa-java.git"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestStripToken_WithoutToken(t *testing.T) {
	url := "https://git.tinycs.cn/tinydsa-java.git"
	if got := stripToken(url); got != url {
		t.Fatalf("stripToken changed a URL with no credentials: got %q", got)
	}
}

// --- buildCommitMessage ---

func TestBuildCommitMessage_Default(t *testing.T) {
	if got := buildCommitMessage("", ""); got != "tinycs submit" {
		t.Fatalf("got %q", got)
	}
}

func TestBuildCommitMessage_WithStage(t *testing.T) {
	if got := buildCommitMessage("", "s03"); got != "tinycs submit [stage=s03]" {
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
	err := formatPushError(&gitPushError{Stderr: "fatal: Authentication failed for 'https://git.tinycs.cn/'"}, "tinydsa", "java")
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

// --- classifyStagedPaths ---

func TestClassifyStagedPaths_Clean(t *testing.T) {
	clean := []string{
		"main.go",
		"src/solver.py",
		"README.md",
		"assets/logo.png", // image — NOT flagged (legitimate binary asset)
		"testdata/sample.bin",
	}
	if got := classifyStagedPaths(clean); len(got) != 0 {
		t.Fatalf("expected no flagged files, got %v", got)
	}
}

func TestClassifyStagedPaths_NodeModules(t *testing.T) {
	paths := []string{"node_modules/lodash/index.js", "src/index.ts"}
	flagged := classifyStagedPaths(paths)
	if len(flagged) != 1 || flagged[0] != "node_modules/lodash/index.js" {
		t.Fatalf("expected node_modules file flagged, got %v", flagged)
	}
}

func TestClassifyStagedPaths_NestedNodeModules(t *testing.T) {
	// nested inside a subdirectory
	paths := []string{"packages/app/node_modules/react/index.js"}
	flagged := classifyStagedPaths(paths)
	if len(flagged) != 1 {
		t.Fatalf("expected nested node_modules flagged, got %v", flagged)
	}
}

func TestClassifyStagedPaths_Pycache(t *testing.T) {
	paths := []string{"__pycache__/solver.cpython-312.pyc", "solver.py"}
	flagged := classifyStagedPaths(paths)
	if len(flagged) != 1 {
		t.Fatalf("expected __pycache__ file flagged, got %v", flagged)
	}
}

func TestClassifyStagedPaths_CompiledExts(t *testing.T) {
	cases := []struct {
		path string
	}{
		{"out/Main.class"},
		{"target/release/main.o"},
		{"build/app.exe"},
		{"lib/native.so"},
		{"dist/solver.pyc"},
	}
	for _, tc := range cases {
		flagged := classifyStagedPaths([]string{tc.path})
		if len(flagged) != 1 {
			t.Errorf("expected %q to be flagged, got %v", tc.path, flagged)
		}
	}
}

func TestClassifyStagedPaths_MixedBatch(t *testing.T) {
	paths := []string{
		"main.go",
		"node_modules/express/index.js",
		"src/Main.class",
		"README.md",
		"__pycache__/foo.pyc",
	}
	flagged := classifyStagedPaths(paths)
	if len(flagged) != 3 {
		t.Fatalf("expected 3 flagged, got %d: %v", len(flagged), flagged)
	}
}

func TestClassifyStagedPaths_VendorAndTarget(t *testing.T) {
	paths := []string{"vendor/github.com/foo/bar/baz.go", "target/debug/app"}
	flagged := classifyStagedPaths(paths)
	if len(flagged) != 2 {
		t.Fatalf("expected 2 flagged, got %v", flagged)
	}
}

// --- checkStagedFiles (integration, real git repo) ---

func TestCheckStagedFiles_IntegrationClean(t *testing.T) {
	dir := t.TempDir()
	mustRunGit(t, dir, "init")
	mustRunGit(t, dir, "config", "user.email", "test@test.com")
	mustRunGit(t, dir, "config", "user.name", "Test")
	writeFile(t, dir, "main.go", "package main\n")
	mustRunGit(t, dir, "add", "-A")

	if err := checkStagedFiles(dir); err != nil {
		t.Fatalf("expected no error for clean files, got: %v", err)
	}
}

func TestCheckStagedFiles_IntegrationFlagged(t *testing.T) {
	dir := t.TempDir()
	mustRunGit(t, dir, "init")
	mustRunGit(t, dir, "config", "user.email", "test@test.com")
	mustRunGit(t, dir, "config", "user.name", "Test")
	writeFile(t, dir, "main.go", "package main\n")
	mkdirWriteFile(t, dir, "node_modules/lodash/index.js", "module.exports={}\n")
	mustRunGit(t, dir, "add", "-A")

	err := checkStagedFiles(dir)
	if err == nil {
		t.Fatal("expected error for node_modules, got nil")
	}
	if !strings.Contains(err.Error(), "node_modules") {
		t.Fatalf("expected node_modules in error, got: %v", err)
	}
}

func TestCheckStagedFiles_TruncatesLongList(t *testing.T) {
	// More than 5 flagged files → "还有 N 个" suffix
	paths := []string{
		"node_modules/a/1.js",
		"node_modules/b/2.js",
		"node_modules/c/3.js",
		"node_modules/d/4.js",
		"node_modules/e/5.js",
		"node_modules/f/6.js",
	}
	flagged := classifyStagedPaths(paths)
	if len(flagged) != 6 {
		t.Fatalf("expected 6 flagged, got %d", len(flagged))
	}
	// Exercise the truncation path via checkStagedFiles by building a real git repo
	dir := t.TempDir()
	mustRunGit(t, dir, "init")
	mustRunGit(t, dir, "config", "user.email", "test@test.com")
	mustRunGit(t, dir, "config", "user.name", "Test")
	for _, p := range paths {
		mkdirWriteFile(t, dir, p, "x\n")
	}
	mustRunGit(t, dir, "add", "-A")

	err := checkStagedFiles(dir)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "还有") {
		t.Fatalf("expected truncation hint '还有', got: %v", err)
	}
}

func TestCheckStagedFiles_SizeLimit(t *testing.T) {
	dir := t.TempDir()
	mustRunGit(t, dir, "init")
	mustRunGit(t, dir, "config", "user.email", "test@test.com")
	mustRunGit(t, dir, "config", "user.name", "Test")

	// Write a file slightly over maxStagedSizeBytes (20 MB)
	bigFile := filepath.Join(dir, "data.bin")
	f, err := os.Create(bigFile)
	if err != nil {
		t.Fatal(err)
	}
	// Write 21 MB of zeros
	chunk := make([]byte, 1<<20) // 1 MB
	for i := 0; i < 21; i++ {
		if _, err := f.Write(chunk); err != nil {
			f.Close()
			t.Fatal(err)
		}
	}
	f.Close()

	mustRunGit(t, dir, "add", "-A")

	err = checkStagedFiles(dir)
	if err == nil {
		t.Fatal("expected size limit error, got nil")
	}
	if !strings.Contains(err.Error(), "超过限制") {
		t.Fatalf("expected 超过限制 in error, got: %v", err)
	}
}

// helpers

func mustRunGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	if err := runGitCmd(dir, args...); err != nil {
		t.Fatalf("git %v: %v", args, err)
	}
}

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func mkdirWriteFile(t *testing.T, dir, relPath, content string) {
	t.Helper()
	full := filepath.Join(dir, filepath.FromSlash(relPath))
	if err := os.MkdirAll(filepath.Dir(full), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}
