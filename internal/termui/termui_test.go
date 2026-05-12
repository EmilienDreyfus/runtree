package termui

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

func TestNonInteractiveStepsUseStableLines(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer
	ui := NewWithOptions(&out, Options{
		Interactive: boolPtr(false),
	})

	ui.Start("Starting main").Success("running main")
	ui.Start("Stopping missing").Skip("skipped missing")
	ui.Start("Restarting broken").Fail("restart failed")

	got := out.String()
	want := "Starting main\n✓ running main\nStopping missing\n- skipped missing\nRestarting broken\n✕ restart failed\n"
	if got != want {
		t.Fatalf("output = %q, want %q", got, want)
	}
	assertNoControlSequences(t, got)
}

func TestInteractiveStepAnimatesAndClearsLine(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer
	ui := NewWithOptions(&out, Options{
		Interactive:   boolPtr(true),
		Color:         boolPtr(false),
		FrameInterval: time.Hour,
	})

	step := ui.Start("Starting main")
	step.Success("running main")

	got := out.String()
	for _, want := range []string{"\r\033[2K", "⠋ Starting main", "✓ running main\n"} {
		if !strings.Contains(got, want) {
			t.Fatalf("output = %q, want substring %q", got, want)
		}
	}
}

func TestNoColorDisablesStatusColors(t *testing.T) {
	t.Setenv("NO_COLOR", "1")

	var out bytes.Buffer
	ui := NewWithOptions(&out, Options{
		Interactive:   boolPtr(true),
		FrameInterval: time.Hour,
	})

	ui.Start("Starting main").Success("running main")

	got := out.String()
	if strings.Contains(got, "\033[32m") || strings.Contains(got, "\033[36m") {
		t.Fatalf("output contains color escape sequence: %q", got)
	}
	if !strings.Contains(got, "\033[2K") {
		t.Fatalf("output = %q, want line clearing escape", got)
	}
}

func boolPtr(value bool) *bool {
	return &value
}

func assertNoControlSequences(t *testing.T, value string) {
	t.Helper()

	if strings.Contains(value, "\r") || strings.Contains(value, "\033") {
		t.Fatalf("output contains terminal control sequence: %q", value)
	}
}
