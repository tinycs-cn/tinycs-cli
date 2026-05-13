package commands

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// testerRunner abstracts running a tester — either as a local binary or via Docker.
// This allows Windows (which can't run the tester natively) to use Docker transparently.
type testerRunner interface {
	// Run executes the tester for the given project directory and stage.
	// If stageSlug is empty and all is false, the tester runs its default behaviour.
	Run(projectDir, stageSlug string, all bool) error
}

// binaryRunner runs the tester as a native binary (macOS / Linux).
type binaryRunner struct {
	path string
}

func (r *binaryRunner) Run(projectDir, stageSlug string, all bool) error {
	args := []string{"-d", projectDir}
	if stageSlug != "" {
		args = append(args, "-s", stageSlug)
	}
	cmd := exec.Command(r.path, args...) //nolint:gosec — path resolved from trusted cache
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// dockerRunner runs the tester inside a Docker container (Windows fallback).
// The project directory is bind-mounted as /workspace inside the container.
type dockerRunner struct {
	course  string
	version string
}

func (r *dockerRunner) Run(projectDir, stageSlug string, all bool) error {
	// Docker image tags follow semver without the "v" prefix (e.g. "0.4.0"),
	// while GitHub Release tags use "v" (e.g. "v0.4.0"). Strip it here.
	imageTag := strings.TrimPrefix(r.version, "v")
	image := fmt.Sprintf("ghcr.io/tinycs-cn/%s-tester:%s", r.course, imageTag)
	args := []string{
		"run", "--rm",
		"-v", projectDir + ":/workspace",
		image,
		"-d", "/workspace",
	}
	if stageSlug != "" {
		args = append(args, "-s", stageSlug)
	}
	cmd := exec.Command("docker", args...) //nolint:gosec
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
