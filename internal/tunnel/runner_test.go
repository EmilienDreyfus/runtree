package tunnel

import (
	"bytes"
	"io"
	"testing"
)

func TestRunnerDiscardsTunnelLogsByDefault(t *testing.T) {
	t.Parallel()

	runner := Runner{}

	if got := runner.stdout(); got != io.Discard {
		t.Fatalf("stdout() = %T, want io.Discard", got)
	}
	if got := runner.stderr(); got != io.Discard {
		t.Fatalf("stderr() = %T, want io.Discard", got)
	}
}

func TestRunnerUsesConfiguredTunnelLogWriters(t *testing.T) {
	t.Parallel()

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	runner := Runner{Stdout: &stdout, Stderr: &stderr}

	if got := runner.stdout(); got != &stdout {
		t.Fatalf("stdout() = %p, want configured stdout", got)
	}
	if got := runner.stderr(); got != &stderr {
		t.Fatalf("stderr() = %p, want configured stderr", got)
	}
}
