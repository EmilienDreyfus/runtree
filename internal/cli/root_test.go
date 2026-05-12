package cli

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"

	"github.com/EmilienDreyfus/runtree/internal/app"
	"github.com/EmilienDreyfus/runtree/internal/buildinfo"
)

func TestVersionCommandPrintsDetailedBuildInfo(t *testing.T) {
	previousVersion := buildinfo.Version
	previousCommit := buildinfo.Commit
	previousDate := buildinfo.Date
	t.Cleanup(func() {
		buildinfo.Version = previousVersion
		buildinfo.Commit = previousCommit
		buildinfo.Date = previousDate
	})

	buildinfo.Version = "v0.1.0"
	buildinfo.Commit = "abc123"
	buildinfo.Date = "2026-05-07T12:00:00Z"

	cmd := NewRootCommand()
	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stdout)
	cmd.SetArgs([]string{"version"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute(version) error = %v", err)
	}

	want := "runtree v0.1.0\ncommit: abc123\nbuilt: 2026-05-07T12:00:00Z\n"
	if got := stdout.String(); got != want {
		t.Fatalf("version output = %q, want %q", got, want)
	}
}

func TestVersionFlagPrintsSummary(t *testing.T) {
	previousVersion := buildinfo.Version
	t.Cleanup(func() {
		buildinfo.Version = previousVersion
	})

	buildinfo.Version = "v0.1.0"

	cmd := NewRootCommand()
	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stdout)
	cmd.SetArgs([]string{"--version"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute(--version) error = %v", err)
	}

	if got := strings.TrimSpace(stdout.String()); got != "runtree v0.1.0" {
		t.Fatalf("--version output = %q", got)
	}
}

func TestHelpDoesNotListScanCommand(t *testing.T) {
	cmd := NewRootCommand()
	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stdout)
	cmd.SetArgs([]string{"--help"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute(--help) error = %v", err)
	}

	if strings.Contains(stdout.String(), "scan") {
		t.Fatalf("help output should not mention scan:\n%s", stdout.String())
	}
}

func TestScanCommandIsUnknown(t *testing.T) {
	cmd := NewRootCommand()
	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stdout)
	cmd.SetArgs([]string{"scan"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("Execute(scan) error = nil, want unknown command")
	}
	if !strings.Contains(err.Error(), "unknown command") {
		t.Fatalf("Execute(scan) error = %v, want unknown command", err)
	}
}

func TestAllLifecycleCommandsUseStableNonTTYProgress(t *testing.T) {
	repoRoot := setupCLITestRepo(t)
	stateHome := filepath.Join(t.TempDir(), "state")

	service := app.NewService(stateHome)
	service.PortChecker = func(int) bool { return true }
	service.Runtime.StartupProbe = 100 * time.Millisecond

	if _, err := service.InitProject(repoRoot, app.InitInput{
		Name:           "demo",
		RunCommand:     `: "{port}"; trap 'exit 0' TERM INT; while true; do sleep 1; done`,
		PortStart:      18100,
		PortEnd:        18199,
		WebURLTemplate: "http://127.0.0.1:{port}",
	}); err != nil {
		t.Fatalf("InitProject() error = %v", err)
	}
	if _, err := service.Inventory(repoRoot, false, true, cliImportAllPrompter{}); err != nil {
		t.Fatalf("Inventory() error = %v", err)
	}
	t.Cleanup(func() {
		_, _ = service.StopInstance(repoRoot, "main")
	})

	t.Chdir(repoRoot)

	upOutput := executeTestCommand(t, newUpCommand(service), "all")
	assertNoTerminalControls(t, upOutput)
	if !strings.Contains(upOutput, "Starting main\n") || !strings.Contains(upOutput, "✓ running main on http://127.0.0.1:18100") {
		t.Fatalf("up all output = %q", upOutput)
	}

	restartOutput := executeTestCommand(t, newRestartCommand(service), "all")
	assertNoTerminalControls(t, restartOutput)
	if !strings.Contains(restartOutput, "Restarting main\n") || !strings.Contains(restartOutput, "✓ restarted main on http://127.0.0.1:18100") {
		t.Fatalf("restart all output = %q", restartOutput)
	}

	downOutput := executeTestCommand(t, newDownCommand(service), "all")
	assertNoTerminalControls(t, downOutput)
	if downOutput != "Stopping main\n✓ stopped main\n" {
		t.Fatalf("down all output = %q", downOutput)
	}
}

type cliImportAllPrompter struct{}

func (cliImportAllPrompter) SelectImports(candidates []app.WorktreeCandidate) ([]app.ImportDecision, error) {
	decisions := make([]app.ImportDecision, 0, len(candidates))
	for _, candidate := range candidates {
		decisions = append(decisions, app.ImportDecision{
			WorktreePath: candidate.WorktreePath,
			Name:         candidate.SuggestedName,
		})
	}
	return decisions, nil
}

func setupCLITestRepo(t *testing.T) string {
	t.Helper()

	repoRoot := t.TempDir()
	runCLITestGit(t, repoRoot, "init", "-b", "main")
	runCLITestGit(t, repoRoot, "config", "user.name", "Runtree Test")
	runCLITestGit(t, repoRoot, "config", "user.email", "runtree@example.com")
	if err := os.WriteFile(filepath.Join(repoRoot, "README.md"), []byte("hello\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(README.md) error = %v", err)
	}
	runCLITestGit(t, repoRoot, "add", "README.md")
	runCLITestGit(t, repoRoot, "commit", "-m", "initial")
	return repoRoot
}

func runCLITestGit(t *testing.T, dir string, args ...string) {
	t.Helper()

	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s error = %v\n%s", strings.Join(args, " "), err, output)
	}
}

func executeTestCommand(t *testing.T, cmd *cobra.Command, args ...string) string {
	t.Helper()

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs(args)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute(%s) error = %v\nstdout:\n%s\nstderr:\n%s", strings.Join(args, " "), err, stdout.String(), stderr.String())
	}
	if stderr.Len() > 0 {
		t.Fatalf("Execute(%s) stderr = %q", strings.Join(args, " "), stderr.String())
	}
	return stdout.String()
}

func assertNoTerminalControls(t *testing.T, output string) {
	t.Helper()

	if strings.Contains(output, "\r") || strings.Contains(output, "\033") {
		t.Fatalf("output contains terminal control sequence: %q", output)
	}
}
