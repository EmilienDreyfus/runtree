package app

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/EmilienDreyfus/runtree/internal/config"
	"github.com/EmilienDreyfus/runtree/internal/state"
)

func TestServiceLifecycleAcrossRealWorktrees(t *testing.T) {
	repoRoot := t.TempDir()
	stateHome := filepath.Join(t.TempDir(), "state")

	runGit(t, repoRoot, "init", "-b", "main")
	runGit(t, repoRoot, "config", "user.name", "Runtree Test")
	runGit(t, repoRoot, "config", "user.email", "runtree@example.com")
	writeFile(t, filepath.Join(repoRoot, "README.md"), "hello\n")
	runGit(t, repoRoot, "add", "README.md")
	runGit(t, repoRoot, "commit", "-m", "initial")

	service := NewService(stateHome)
	service.PortChecker = func(int) bool { return true }
	initInput := InitInput{
		Name:           "goodays-api",
		RunCommand:     `trap 'exit 0' TERM INT; printf "boot %s\n" "{port}"; while true; do printf "tick %s\n" "{port}"; sleep 1; done`,
		PortStart:      8100,
		PortEnd:        8199,
		WebURLTemplate: "http://127.0.0.1:{port}",
	}
	ctx, err := service.InitProject(repoRoot, initInput)
	if err != nil {
		t.Fatalf("InitProject() error = %v", err)
	}
	expectedConfigPath, err := filepath.EvalSymlinks(filepath.Join(repoRoot, config.FileName))
	if err != nil {
		t.Fatalf("EvalSymlinks(config path) error = %v", err)
	}
	actualConfigPath, err := filepath.EvalSymlinks(ctx.ConfigPath)
	if err != nil {
		t.Fatalf("EvalSymlinks(actual config path) error = %v", err)
	}
	if actualConfigPath != expectedConfigPath {
		t.Fatalf("ConfigPath = %s, want %s", actualConfigPath, expectedConfigPath)
	}

	configBytes, err := os.ReadFile(ctx.ConfigPath)
	if err != nil {
		t.Fatalf("ReadFile(%s) error = %v", ctx.ConfigPath, err)
	}
	if strings.Contains(string(configBytes), repoRoot) {
		t.Fatalf("config should remain portable, got %q", string(configBytes))
	}

	linkedWorktree := filepath.Join(t.TempDir(), "repo-auth")
	runGit(t, repoRoot, "worktree", "add", linkedWorktree, "-b", "feat/auth-refactor")
	cursorDetachedWorktree := filepath.Join(repoRoot, ".cursor", "worktrees", "goodays-api", "cbm")
	runGit(t, repoRoot, "worktree", "add", "--detach", cursorDetachedWorktree, "HEAD")
	seedLegacyCursorInstance(t, stateHome, ctx.Project, cursorDetachedWorktree)

	discovery, err := service.ScanProject(repoRoot, false, nil)
	if err != nil {
		t.Fatalf("ScanProject(discovery) error = %v", err)
	}
	if !discovery.DiscoveryOnly {
		t.Fatal("ScanProject(discovery).DiscoveryOnly = false, want true")
	}
	if len(discovery.Imported) != 0 {
		t.Fatalf("ScanProject(discovery).Imported = %d, want 0", len(discovery.Imported))
	}
	if len(discovery.Candidates) != 2 {
		t.Fatalf("ScanProject(discovery).Candidates = %d, want 2", len(discovery.Candidates))
	}
	if len(discovery.Updated) == 0 {
		t.Fatal("ScanProject(discovery).Updated = 0, want cursor legacy instance reclassified")
	}
	for _, candidate := range discovery.Candidates {
		if candidate.WorktreePath == cursorDetachedWorktree {
			t.Fatalf("detached cursor worktree should be ignored, got candidate %+v", candidate)
		}
	}

	imported, err := service.ScanProject(repoRoot, true, approveAllPrompter{})
	if err != nil {
		t.Fatalf("ScanProject(import) error = %v", err)
	}
	if len(imported.Imported) != 2 {
		t.Fatalf("ScanProject(import).Imported = %d, want 2", len(imported.Imported))
	}

	instances, err := service.ListInstances(repoRoot, false)
	if err != nil {
		t.Fatalf("ListInstances() error = %v", err)
	}
	if len(instances) != 2 {
		t.Fatalf("len(instances) = %d, want 2", len(instances))
	}

	allInstances, err := service.ListInstances(repoRoot, true)
	if err != nil {
		t.Fatalf("ListInstances(all) error = %v", err)
	}
	if len(allInstances) != 3 {
		t.Fatalf("len(allInstances) = %d, want 3", len(allInstances))
	}
	cursor := findInstance(t, allInstances, "cbm")
	if cursor.Visibility != state.VisibilityIgnored || cursor.Source != state.SourceCursor {
		t.Fatalf("legacy cursor instance = %+v, want ignored cursor source", cursor)
	}

	portsByName := make(map[string]int, len(instances))
	for _, instance := range instances {
		portsByName[instance.Name] = instance.Port
	}
	if portsByName["main"] != 8100 {
		t.Fatalf("main port = %d, want 8100", portsByName["main"])
	}
	if portsByName["auth-refactor"] != 8101 {
		t.Fatalf("auth-refactor port = %d, want 8101", portsByName["auth-refactor"])
	}

	secondScan, err := service.ScanProject(repoRoot, true, approveAllPrompter{})
	if err != nil {
		t.Fatalf("ScanProject(second) error = %v", err)
	}
	if len(secondScan.Imported) != 0 || len(secondScan.Candidates) != 0 {
		t.Fatalf("second scan imported=%d candidates=%d, want 0/0", len(secondScan.Imported), len(secondScan.Candidates))
	}

	running, err := service.StartInstance(repoRoot, "auth-refactor")
	if err != nil {
		t.Fatalf("StartInstance() error = %v", err)
	}
	if running.Status != state.StatusRunning || running.PID == 0 {
		t.Fatalf("running instance = %+v", running)
	}

	waitFor(t, 5*time.Second, func() bool {
		data, err := os.ReadFile(running.LogPath)
		return err == nil && strings.Contains(string(data), "tick 8101")
	})

	stopped, err := service.StopInstance(repoRoot, "auth-refactor")
	if err != nil {
		t.Fatalf("StopInstance() error = %v", err)
	}
	if stopped.Status != state.StatusStopped || stopped.PID != 0 {
		t.Fatalf("stopped instance = %+v", stopped)
	}

	restarted, err := service.RestartInstance(repoRoot, "auth-refactor")
	if err != nil {
		t.Fatalf("RestartInstance() error = %v", err)
	}
	if restarted.Status != state.StatusRunning || restarted.PID == 0 {
		t.Fatalf("restarted instance = %+v", restarted)
	}
	if _, err := service.StopInstance(repoRoot, "auth-refactor"); err != nil {
		t.Fatalf("StopInstance(after restart) error = %v", err)
	}

	service.PortChecker = func(port int) bool { return port != portsByName["main"] }
	_, err = service.StartInstance(repoRoot, "main")
	service.PortChecker = func(int) bool { return true }
	if err == nil || !strings.Contains(err.Error(), "already in use") {
		t.Fatalf("StartInstance(main) error = %v, want port conflict", err)
	}

	runGit(t, repoRoot, "worktree", "remove", "--force", linkedWorktree)
	if _, err := service.ScanProject(repoRoot, false, nil); err != nil {
		t.Fatalf("ScanProject(after remove) error = %v", err)
	}

	instances, err = service.ListInstances(repoRoot, false)
	if err != nil {
		t.Fatalf("ListInstances(after remove) error = %v", err)
	}
	auth := findInstance(t, instances, "auth-refactor")
	if auth.Status != state.StatusMissing {
		t.Fatalf("auth-refactor status = %s, want %s", auth.Status, state.StatusMissing)
	}

	pruned, err := service.PruneInstances(repoRoot)
	if err != nil {
		t.Fatalf("PruneInstances() error = %v", err)
	}
	if len(pruned.Pruned) != 1 || pruned.Pruned[0].Name != "cbm" {
		t.Fatalf("PruneInstances() = %+v, want cbm pruned", pruned)
	}

	allInstances, err = service.ListInstances(repoRoot, true)
	if err != nil {
		t.Fatalf("ListInstances(all after prune) error = %v", err)
	}
	if len(allInstances) != 2 {
		t.Fatalf("len(allInstances after prune) = %d, want 2", len(allInstances))
	}
}

func TestStartInstanceFailsWhenProcessExitsImmediately(t *testing.T) {
	repoRoot := t.TempDir()
	stateHome := filepath.Join(t.TempDir(), "state")

	runGit(t, repoRoot, "init", "-b", "main")
	runGit(t, repoRoot, "config", "user.name", "Runtree Test")
	runGit(t, repoRoot, "config", "user.email", "runtree@example.com")
	writeFile(t, filepath.Join(repoRoot, "README.md"), "hello\n")
	runGit(t, repoRoot, "add", "README.md")
	runGit(t, repoRoot, "commit", "-m", "initial")

	service := NewService(stateHome)
	service.PortChecker = func(int) bool { return true }
	service.Runtime.StartupProbe = 100 * time.Millisecond

	_, err := service.InitProject(repoRoot, InitInput{
		Name:           "broken-api",
		RunCommand:     `printf 'boom {port}\n'; exit 1`,
		PortStart:      8100,
		PortEnd:        8199,
		WebURLTemplate: "http://127.0.0.1:{port}",
	})
	if err != nil {
		t.Fatalf("InitProject() error = %v", err)
	}

	imported, err := service.ScanProject(repoRoot, true, approveAllPrompter{})
	if err != nil {
		t.Fatalf("ScanProject() error = %v", err)
	}
	if len(imported.Imported) != 1 {
		t.Fatalf("ScanProject().Imported = %d, want 1", len(imported.Imported))
	}

	_, err = service.StartInstance(repoRoot, "main")
	if err == nil {
		t.Fatal("StartInstance() error = nil, want fast-exit error")
	}
	if !strings.Contains(err.Error(), "process exited during startup") || !strings.Contains(err.Error(), "boom 8100") {
		t.Fatalf("StartInstance() error = %v, want fast-exit log tail", err)
	}

	instances, err := service.ListInstances(repoRoot, true)
	if err != nil {
		t.Fatalf("ListInstances() error = %v", err)
	}
	main := findInstance(t, instances, "main")
	if main.Status != state.StatusStopped || main.PID != 0 {
		t.Fatalf("main instance after fast-exit = %+v", main)
	}
}

type approveAllPrompter struct{}

func (approveAllPrompter) ReviewCandidate(candidate ScanCandidate) (bool, string, error) {
	return true, candidate.SuggestedName, nil
}

func runGit(t *testing.T, dir string, args ...string) string {
	t.Helper()

	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, output)
	}
	return strings.TrimSpace(string(output))
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile(%s) error = %v", path, err)
	}
}

func seedLegacyCursorInstance(t *testing.T, stateHome string, project state.Project, worktreePath string) {
	t.Helper()

	canonicalWorktreePath, err := filepath.EvalSymlinks(worktreePath)
	if err != nil {
		t.Fatalf("EvalSymlinks(%s) error = %v", worktreePath, err)
	}

	store, err := state.Open(stateHome)
	if err != nil {
		t.Fatalf("state.Open() error = %v", err)
	}
	defer store.Close()

	if err := store.EnsureLogDir(project.Name); err != nil {
		t.Fatalf("EnsureLogDir() error = %v", err)
	}
	_, err = store.CreateInstance(state.Instance{
		ProjectID:    project.ID,
		Name:         "cbm",
		Branch:       "",
		WorktreePath: canonicalWorktreePath,
		Source:       state.SourceManual,
		Visibility:   state.VisibilityVisible,
		Port:         9001,
		PID:          0,
		Status:       state.StatusStopped,
		LogPath:      store.LogPath(project.Name, "cbm"),
	})
	if err != nil {
		t.Fatalf("CreateInstance(legacy cursor) error = %v", err)
	}
}

func waitFor(t *testing.T, timeout time.Duration, predicate func() bool) {
	t.Helper()

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if predicate() {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("condition was not met before timeout")
}

func findInstance(t *testing.T, instances []state.Instance, name string) state.Instance {
	t.Helper()

	for _, instance := range instances {
		if instance.Name == name {
			return instance
		}
	}
	t.Fatalf("instance %s not found", name)
	return state.Instance{}
}
