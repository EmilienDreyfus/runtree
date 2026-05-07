package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteLoadAndFindConfig(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	cfg := Default()
	cfg.Project.Name = "goodays-api"
	cfg.Run.Command = "uv run python manage.py runserver 127.0.0.1:{port}"

	path := filepath.Join(root, FileName)
	if err := Write(path, cfg); err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	subdir := filepath.Join(root, "nested", "dir")
	if err := os.MkdirAll(subdir, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}

	found, err := Find(subdir)
	if err != nil {
		t.Fatalf("Find() error = %v", err)
	}
	if found != path {
		t.Fatalf("Find() = %s, want %s", found, path)
	}

	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if loaded.Project.Name != cfg.Project.Name {
		t.Fatalf("Project.Name = %q, want %q", loaded.Project.Name, cfg.Project.Name)
	}
	if got := loaded.RenderRunCommand(8123); !strings.Contains(got, "8123") {
		t.Fatalf("RenderRunCommand() = %q, want rendered port", got)
	}
	if got := loaded.RenderWebURL(8123); got != "http://127.0.0.1:8123" {
		t.Fatalf("RenderWebURL() = %q", got)
	}
}

func TestValidateRequiresPortPlaceholder(t *testing.T) {
	t.Parallel()

	cfg := Default()
	cfg.Project.Name = "goodays-api"
	cfg.Run.Command = "uv run python manage.py runserver"

	if err := cfg.Validate(); err == nil {
		t.Fatal("Validate() error = nil, want placeholder validation error")
	}
}
