package app

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/EmilienDreyfus/runtree/internal/gitutil"
)

func TestClassifyWorktree(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	mainPath := filepath.Join(root, "repo")
	cursorDetachedPath := filepath.Join(root, ".cursor", "worktrees", "repo", "cbm")
	cursorBranchPath := filepath.Join(root, ".cursor", "worktrees", "repo", "feature")
	claudeDetachedPath := filepath.Join(root, ".claude", "worktrees", "repo", "romantic-ishizaka")
	codexDetachedPath := filepath.Join(root, ".codex", "worktrees", "7715", "repo")
	missingPath := filepath.Join(root, "missing")

	for _, path := range []string{mainPath, cursorDetachedPath, cursorBranchPath, claudeDetachedPath, codexDetachedPath} {
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatalf("MkdirAll(%s) error = %v", path, err)
		}
	}

	tests := []struct {
		name           string
		worktree       gitutil.Worktree
		wantSource     string
		wantVisibility string
	}{
		{
			name: "main worktree with branch is imported",
			worktree: gitutil.Worktree{
				Path:   mainPath,
				Branch: "master",
				Main:   true,
			},
			wantSource:     "main",
			wantVisibility: "visible",
		},
		{
			name: "detached cursor worktree is ignored",
			worktree: gitutil.Worktree{
				Path:     cursorDetachedPath,
				Detached: true,
			},
			wantSource:     "cursor",
			wantVisibility: "ignored",
		},
		{
			name: "branch named cursor worktree is imported",
			worktree: gitutil.Worktree{
				Path:     cursorBranchPath,
				Branch:   "feature/something",
				Detached: false,
			},
			wantSource:     "cursor",
			wantVisibility: "visible",
		},
		{
			name: "detached claude worktree is ignored",
			worktree: gitutil.Worktree{
				Path:     claudeDetachedPath,
				Detached: true,
			},
			wantSource:     "claude",
			wantVisibility: "ignored",
		},
		{
			name: "detached codex worktree is preserved",
			worktree: gitutil.Worktree{
				Path:     codexDetachedPath,
				Detached: true,
			},
			wantSource:     "codex",
			wantVisibility: "visible",
		},
		{
			name: "missing path is ignored",
			worktree: gitutil.Worktree{
				Path:   missingPath,
				Branch: "feature/missing",
			},
			wantSource:     "manual",
			wantVisibility: "ignored",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			gotSource, gotVisibility := classifyWorktree(tt.worktree)
			if gotSource != tt.wantSource || gotVisibility != tt.wantVisibility {
				t.Fatalf(
					"classifyWorktree(%+v) = (%s, %s), want (%s, %s)",
					tt.worktree,
					gotSource,
					gotVisibility,
					tt.wantSource,
					tt.wantVisibility,
				)
			}
		})
	}
}
