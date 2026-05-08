package cli

import (
	"bytes"
	"strings"
	"testing"

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
