package gitutil

import "testing"

func TestParseWorktreeList(t *testing.T) {
	t.Parallel()

	output := []byte(`worktree /tmp/repo
HEAD abcdef
branch refs/heads/main

worktree /tmp/repo-auth
HEAD bcdefa
branch refs/heads/feat/auth-refactor

worktree /tmp/repo-detached
HEAD cdefab
detached
`)

	worktrees, err := ParseWorktreeList(output)
	if err != nil {
		t.Fatalf("ParseWorktreeList() error = %v", err)
	}
	if len(worktrees) != 3 {
		t.Fatalf("len(worktrees) = %d, want 3", len(worktrees))
	}
	if !worktrees[0].Main || worktrees[0].Branch != "main" {
		t.Fatalf("main worktree parsed incorrectly: %+v", worktrees[0])
	}
	if worktrees[1].Branch != "feat/auth-refactor" {
		t.Fatalf("branch = %q, want feat/auth-refactor", worktrees[1].Branch)
	}
	if !worktrees[2].Detached {
		t.Fatalf("detached worktree not marked detached: %+v", worktrees[2])
	}
}
