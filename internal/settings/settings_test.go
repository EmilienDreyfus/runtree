package settings

import (
	"path/filepath"
	"testing"
)

func TestSaveAndLoadSettings(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	input := Settings{
		EditorCommand: "open -a '/Applications/Cursor.app' {path}",
	}

	if err := Save(home, input); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	loaded, err := Load(home)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if loaded.EditorCommand != input.EditorCommand {
		t.Fatalf("Load().EditorCommand = %q, want %q", loaded.EditorCommand, input.EditorCommand)
	}

	path := filepath.Join(home, fileName)
	if _, err := Load(filepath.Dir(path)); err != nil {
		// sanity check that loading still works from same home path
		t.Fatalf("Load(home) sanity check error = %v", err)
	}
}

func TestValidateEditorCommandRequiresPathPlaceholder(t *testing.T) {
	t.Parallel()

	err := Settings{EditorCommand: "open -a Cursor"}.Validate()
	if err == nil {
		t.Fatal("Validate() error = nil, want placeholder validation error")
	}
}
