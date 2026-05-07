package openers

import (
	"strings"
	"testing"
)

func TestResolveEditorUsesEnvTemplate(t *testing.T) {
	t.Setenv("RUNTREE_EDITOR", "cursor {path}")

	spec, err := ResolveEditor("/tmp/my repo", "")
	if err != nil {
		t.Fatalf("ResolveEditor() error = %v", err)
	}
	if spec.Name != "/bin/sh" {
		t.Fatalf("ResolveEditor().Name = %q, want /bin/sh", spec.Name)
	}
	if len(spec.Args) != 2 || !strings.Contains(spec.Args[1], "'/tmp/my repo'") {
		t.Fatalf("ResolveEditor().Args = %#v, want quoted path", spec.Args)
	}
}

func TestResolveEditorRejectsMissingPlaceholder(t *testing.T) {
	t.Setenv("RUNTREE_EDITOR", "cursor")
	if _, err := ResolveEditor("/tmp/repo", ""); err == nil {
		t.Fatal("ResolveEditor() error = nil, want placeholder validation error")
	}
}

func TestResolveEditorUsesPreferredCommand(t *testing.T) {
	t.Setenv("RUNTREE_EDITOR", "")

	spec, err := ResolveEditor("/tmp/repo", "open -a '/Applications/Cursor.app' {path}")
	if err != nil {
		t.Fatalf("ResolveEditor() error = %v", err)
	}
	if spec.Name != "/bin/sh" {
		t.Fatalf("ResolveEditor().Name = %q, want /bin/sh", spec.Name)
	}
	if len(spec.Args) != 2 || !strings.Contains(spec.Args[1], "Cursor.app") {
		t.Fatalf("ResolveEditor().Args = %#v, want preferred command", spec.Args)
	}
}

func TestRunReturnsFastFailureOutput(t *testing.T) {
	spec := ExecSpec{
		Name: "/bin/sh",
		Args: []string{"-lc", "echo boom >&2; exit 1"},
	}

	err := Run(spec)
	if err == nil {
		t.Fatal("Run() error = nil, want fast failure")
	}
	if !strings.Contains(err.Error(), "boom") {
		t.Fatalf("Run() error = %v, want stderr output", err)
	}
}
