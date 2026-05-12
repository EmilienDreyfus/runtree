package app

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/EmilienDreyfus/runtree/internal/config"
	"github.com/EmilienDreyfus/runtree/internal/gitutil"
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
		Name:           "newProject",
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
	cursorDetachedWorktree := filepath.Join(repoRoot, ".cursor", "worktrees", "newProject", "cbm")
	runGit(t, repoRoot, "worktree", "add", "--detach", cursorDetachedWorktree, "HEAD")
	seedLegacyCursorInstance(t, stateHome, ctx.Project, cursorDetachedWorktree)

	discovery, err := service.Inventory(repoRoot, false, false, nil)
	if err != nil {
		t.Fatalf("Inventory(discovery) error = %v", err)
	}
	if len(discovery.Imported) != 0 {
		t.Fatalf("Inventory(discovery).Imported = %d, want 0", len(discovery.Imported))
	}
	if len(discovery.Candidates) != 2 {
		t.Fatalf("Inventory(discovery).Candidates = %d, want 2", len(discovery.Candidates))
	}
	if len(discovery.Updated) == 0 {
		t.Fatal("Inventory(discovery).Updated = 0, want cursor legacy instance reclassified")
	}
	for _, candidate := range discovery.Candidates {
		if candidate.WorktreePath == cursorDetachedWorktree {
			t.Fatalf("detached cursor worktree should be ignored, got candidate %+v", candidate)
		}
	}

	imported, err := service.Inventory(repoRoot, false, true, importAllPrompter{})
	if err != nil {
		t.Fatalf("Inventory(import) error = %v", err)
	}
	if len(imported.Imported) != 2 {
		t.Fatalf("Inventory(import).Imported = %d, want 2", len(imported.Imported))
	}
	if len(imported.Instances) != 2 {
		t.Fatalf("Inventory(import).Instances = %d, want 2", len(imported.Instances))
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

	secondInventory, err := service.Inventory(repoRoot, false, true, importAllPrompter{})
	if err != nil {
		t.Fatalf("Inventory(second) error = %v", err)
	}
	if len(secondInventory.Imported) != 0 || len(secondInventory.Candidates) != 0 {
		t.Fatalf("second inventory imported=%d candidates=%d, want 0/0", len(secondInventory.Imported), len(secondInventory.Candidates))
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
	if _, err := service.Inventory(repoRoot, false, false, nil); err != nil {
		t.Fatalf("Inventory(after remove) error = %v", err)
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

	imported, err := service.Inventory(repoRoot, false, true, importAllPrompter{})
	if err != nil {
		t.Fatalf("Inventory() error = %v", err)
	}
	if len(imported.Imported) != 1 {
		t.Fatalf("Inventory().Imported = %d, want 1", len(imported.Imported))
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

func TestInventoryKeepsManuallyDeletedWorktreeVisibleAsMissing(t *testing.T) {
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
	ctx, err := service.InitProject(repoRoot, InitInput{
		Name:           "newProject",
		RunCommand:     `printf "boot %s\n" "{port}"`,
		PortStart:      8100,
		PortEnd:        8199,
		WebURLTemplate: "http://127.0.0.1:{port}",
	})
	if err != nil {
		t.Fatalf("InitProject() error = %v", err)
	}

	linkedWorktree := filepath.Join(t.TempDir(), "repo-auth")
	runGit(t, repoRoot, "worktree", "add", linkedWorktree, "-b", "feat/auth")
	if _, err := service.Inventory(repoRoot, false, true, importAllPrompter{}); err != nil {
		t.Fatalf("Inventory(import) error = %v", err)
	}

	if err := os.RemoveAll(linkedWorktree); err != nil {
		t.Fatalf("RemoveAll(%s) error = %v", linkedWorktree, err)
	}

	store, err := state.Open(stateHome)
	if err != nil {
		t.Fatalf("state.Open() error = %v", err)
	}
	instances, err := store.InstancesByProject(ctx.Project.ID)
	if err != nil {
		t.Fatalf("InstancesByProject() error = %v", err)
	}
	authBefore := findInstance(t, instances, "auth")
	authBefore.Status = state.StatusMissing
	authBefore.Visibility = state.VisibilityIgnored
	if err := store.UpdateInstance(authBefore); err != nil {
		t.Fatalf("UpdateInstance(auth) error = %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("store.Close() error = %v", err)
	}

	result, err := service.Inventory(repoRoot, false, false, nil)
	if err != nil {
		t.Fatalf("Inventory(after manual delete) error = %v", err)
	}
	if len(result.Candidates) != 0 {
		t.Fatalf("Inventory(after manual delete).Candidates = %+v, want none", result.Candidates)
	}
	auth := findInstance(t, result.Instances, "auth")
	if auth.Status != state.StatusMissing {
		t.Fatalf("auth status = %s, want %s", auth.Status, state.StatusMissing)
	}
	if auth.Visibility != state.VisibilityVisible {
		t.Fatalf("auth visibility = %s, want %s", auth.Visibility, state.VisibilityVisible)
	}
}

func TestRunInInstanceUsesWorktreeAndIsolatedEnv(t *testing.T) {
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
	if _, err := service.InitProject(repoRoot, InitInput{
		Name:           "runner",
		RunCommand:     `printf "boot {port}\n"`,
		PortStart:      8100,
		PortEnd:        8199,
		WebURLTemplate: "http://127.0.0.1:{port}",
	}); err != nil {
		t.Fatalf("InitProject() error = %v", err)
	}
	if _, err := service.Inventory(repoRoot, false, true, importAllPrompter{}); err != nil {
		t.Fatalf("Inventory() error = %v", err)
	}

	activeVenv := filepath.Join(t.TempDir(), ".venv")
	t.Setenv("VIRTUAL_ENV", activeVenv)
	t.Setenv("POETRY_ACTIVE", "1")
	t.Setenv("PATH", filepath.Join(activeVenv, "bin")+string(filepath.ListSeparator)+os.Getenv("PATH"))
	t.Setenv("SECRET_KEY", "kept")

	var stdout bytes.Buffer
	err := service.RunInInstance(repoRoot, "main", RunCommandInput{
		Args: []string{"sh", "-c", strings.Join([]string{
			"pwd",
			`printf "port=%s\n" "$RUNTREE_PORT"`,
			`printf "instance=%s\n" "$RUNTREE_INSTANCE"`,
			`printf "project=%s\n" "$RUNTREE_PROJECT"`,
			`printf "secret=%s\n" "$SECRET_KEY"`,
			`printf "virtual=%s\n" "${VIRTUAL_ENV:-}"`,
			`printf "poetry=%s\n" "${POETRY_ACTIVE:-}"`,
			`printf "path=%s\n" "$PATH"`,
		}, "; ")},
		Stdout: &stdout,
	})
	if err != nil {
		t.Fatalf("RunInInstance() error = %v", err)
	}

	output := stdout.String()
	canonicalRepoRoot, err := gitutil.CanonicalPath(repoRoot)
	if err != nil {
		t.Fatalf("CanonicalPath(%s) error = %v", repoRoot, err)
	}
	if !strings.Contains(output, canonicalRepoRoot+"\n") {
		t.Fatalf("RunInInstance() output = %q, want cwd %s", output, canonicalRepoRoot)
	}
	for _, want := range []string{
		"port=8100",
		"instance=main",
		"project=runner",
		"secret=kept",
		"virtual=",
		"poetry=",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("RunInInstance() output = %q, want %q", output, want)
		}
	}
	if strings.Contains(output, filepath.Join(activeVenv, "bin")) {
		t.Fatalf("RunInInstance() leaked active venv path in output %q", output)
	}
}

func TestInventoryCanLinkIgnoredRuntimeFilesDuringImport(t *testing.T) {
	repoRoot := t.TempDir()
	stateHome := filepath.Join(t.TempDir(), "state")

	runGit(t, repoRoot, "init", "-b", "main")
	runGit(t, repoRoot, "config", "user.name", "Runtree Test")
	runGit(t, repoRoot, "config", "user.email", "runtree@example.com")
	writeFile(t, filepath.Join(repoRoot, ".gitignore"), ".env\n.env.local\n")
	writeFile(t, filepath.Join(repoRoot, "README.md"), "hello\n")
	runGit(t, repoRoot, "add", ".gitignore", "README.md")
	runGit(t, repoRoot, "commit", "-m", "initial")
	writeFile(t, filepath.Join(repoRoot, ".env"), "SECRET_KEY=main\n")
	writeFile(t, filepath.Join(repoRoot, ".env.local"), "DEBUG=1\n")

	service := NewService(stateHome)
	service.PortChecker = func(int) bool { return true }
	if _, err := service.InitProject(repoRoot, InitInput{
		Name:           "env-project",
		RunCommand:     `printf "boot {port}\n"`,
		PortStart:      8100,
		PortEnd:        8199,
		WebURLTemplate: "http://127.0.0.1:{port}",
	}); err != nil {
		t.Fatalf("InitProject() error = %v", err)
	}

	linkedWorktree := filepath.Join(t.TempDir(), "repo-auth")
	runGit(t, repoRoot, "worktree", "add", linkedWorktree, "-b", "feat/auth")
	canonicalLinkedWorktree, err := gitutil.CanonicalPath(linkedWorktree)
	if err != nil {
		t.Fatalf("CanonicalPath(%s) error = %v", linkedWorktree, err)
	}
	canonicalRepoRoot, err := gitutil.CanonicalPath(repoRoot)
	if err != nil {
		t.Fatalf("CanonicalPath(%s) error = %v", repoRoot, err)
	}

	discovery, err := service.Inventory(repoRoot, false, false, nil)
	if err != nil {
		t.Fatalf("Inventory(discovery) error = %v", err)
	}
	var linkedCandidate *WorktreeCandidate
	for i := range discovery.Candidates {
		if discovery.Candidates[i].WorktreePath == canonicalLinkedWorktree {
			linkedCandidate = &discovery.Candidates[i]
		}
	}
	if linkedCandidate == nil {
		t.Fatalf("Inventory(discovery).Candidates = %+v, want linked worktree", discovery.Candidates)
	}
	if got := runtimeFilePaths(linkedCandidate.RuntimeFiles); strings.Join(got, ",") != ".env,.env.local" {
		t.Fatalf("candidate runtime files = %v, want .env and .env.local", got)
	}

	result, err := service.Inventory(repoRoot, false, true, importPathsPrompter{
		paths: map[string]string{
			canonicalLinkedWorktree: "auth",
		},
		runtimeAction: RuntimeFileSymlink,
	})
	if err != nil {
		t.Fatalf("Inventory(import) error = %v", err)
	}
	if len(result.Imported) != 1 {
		t.Fatalf("Inventory(import).Imported = %d, want 1", len(result.Imported))
	}

	for _, relPath := range []string{".env", ".env.local"} {
		target := filepath.Join(canonicalLinkedWorktree, relPath)
		linkTarget, err := os.Readlink(target)
		if err != nil {
			t.Fatalf("Readlink(%s) error = %v", target, err)
		}
		want := filepath.Join(canonicalRepoRoot, relPath)
		if linkTarget != want {
			t.Fatalf("Readlink(%s) = %s, want %s", target, linkTarget, want)
		}
	}
}

func TestInventoryReportsManagedRuntimeFileTargets(t *testing.T) {
	repoRoot := t.TempDir()
	stateHome := filepath.Join(t.TempDir(), "state")

	runGit(t, repoRoot, "init", "-b", "main")
	runGit(t, repoRoot, "config", "user.name", "Runtree Test")
	runGit(t, repoRoot, "config", "user.email", "runtree@example.com")
	writeFile(t, filepath.Join(repoRoot, ".gitignore"), ".env\n")
	writeFile(t, filepath.Join(repoRoot, "README.md"), "hello\n")
	runGit(t, repoRoot, "add", ".gitignore", "README.md")
	runGit(t, repoRoot, "commit", "-m", "initial")
	writeFile(t, filepath.Join(repoRoot, ".env"), "SECRET_KEY=main\n")

	service := NewService(stateHome)
	service.PortChecker = func(int) bool { return true }
	if _, err := service.InitProject(repoRoot, InitInput{
		Name:           "managed-env",
		RunCommand:     `printf "boot {port}\n"`,
		PortStart:      8100,
		PortEnd:        8199,
		WebURLTemplate: "http://127.0.0.1:{port}",
	}); err != nil {
		t.Fatalf("InitProject() error = %v", err)
	}

	linkedWorktree := filepath.Join(t.TempDir(), "repo-auth")
	runGit(t, repoRoot, "worktree", "add", linkedWorktree, "-b", "feat/auth")
	canonicalLinkedWorktree, err := gitutil.CanonicalPath(linkedWorktree)
	if err != nil {
		t.Fatalf("CanonicalPath(%s) error = %v", linkedWorktree, err)
	}

	if _, err := service.Inventory(repoRoot, false, true, importPathsPrompter{
		paths: map[string]string{
			canonicalLinkedWorktree: "auth",
		},
	}); err != nil {
		t.Fatalf("Inventory(import) error = %v", err)
	}

	result, err := service.Inventory(repoRoot, false, false, nil)
	if err != nil {
		t.Fatalf("Inventory(managed) error = %v", err)
	}
	if len(result.RuntimeFileTargets) != 1 {
		t.Fatalf("RuntimeFileTargets = %+v, want one managed target", result.RuntimeFileTargets)
	}
	target := result.RuntimeFileTargets[0]
	if target.WorktreePath != canonicalLinkedWorktree || strings.Join(runtimeFilePaths(target.RuntimeFiles), ",") != ".env" {
		t.Fatalf("RuntimeFileTargets[0] = %+v, want auth .env target", target)
	}

	configured, err := service.ConfigureRuntimeFiles(repoRoot, []RuntimeFileTargetDecision{{
		WorktreePath: canonicalLinkedWorktree,
		RuntimeFiles: []RuntimeFileDecision{{
			Path:   ".env",
			Action: RuntimeFileSymlink,
		}},
	}})
	if err != nil {
		t.Fatalf("ConfigureRuntimeFiles() error = %v", err)
	}
	if len(configured) != 1 {
		t.Fatalf("ConfigureRuntimeFiles() configured %d targets, want 1", len(configured))
	}
	if _, err := os.Readlink(filepath.Join(canonicalLinkedWorktree, ".env")); err != nil {
		t.Fatalf("Readlink(.env) error = %v", err)
	}
}

func TestInventoryImportsSelectedWorktreesOnly(t *testing.T) {
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
	if _, err := service.InitProject(repoRoot, InitInput{
		Name:           "partial-import",
		RunCommand:     `printf "boot {port}\n"`,
		PortStart:      8100,
		PortEnd:        8199,
		WebURLTemplate: "http://127.0.0.1:{port}",
	}); err != nil {
		t.Fatalf("InitProject() error = %v", err)
	}

	authWorktree := filepath.Join(t.TempDir(), "repo-auth")
	billingWorktree := filepath.Join(t.TempDir(), "repo-billing")
	runGit(t, repoRoot, "worktree", "add", authWorktree, "-b", "feat/auth")
	runGit(t, repoRoot, "worktree", "add", billingWorktree, "-b", "feat/billing")
	canonicalAuthWorktree, err := gitutil.CanonicalPath(authWorktree)
	if err != nil {
		t.Fatalf("CanonicalPath(%s) error = %v", authWorktree, err)
	}

	result, err := service.Inventory(repoRoot, false, true, importPathsPrompter{
		paths: map[string]string{
			canonicalAuthWorktree: "auth",
		},
	})
	if err != nil {
		t.Fatalf("Inventory(partial import) error = %v", err)
	}
	if len(result.Imported) != 1 || result.Imported[0].Name != "auth" {
		t.Fatalf("Inventory(partial import).Imported = %+v, want auth only", result.Imported)
	}
	if len(result.Instances) != 1 {
		t.Fatalf("Inventory(partial import).Instances = %d, want 1", len(result.Instances))
	}

	discovery, err := service.Inventory(repoRoot, false, false, nil)
	if err != nil {
		t.Fatalf("Inventory(discovery after partial import) error = %v", err)
	}
	if len(discovery.Candidates) != 2 {
		t.Fatalf("Inventory(discovery after partial import).Candidates = %d, want 2", len(discovery.Candidates))
	}
	for _, candidate := range discovery.Candidates {
		if candidate.WorktreePath == canonicalAuthWorktree {
			t.Fatalf("imported worktree should not remain a candidate: %+v", candidate)
		}
	}
}

type importAllPrompter struct{}

func (importAllPrompter) SelectImports(candidates []WorktreeCandidate) ([]ImportDecision, error) {
	decisions := make([]ImportDecision, 0, len(candidates))
	for _, candidate := range candidates {
		decisions = append(decisions, ImportDecision{
			WorktreePath: candidate.WorktreePath,
			Name:         candidate.SuggestedName,
		})
	}
	return decisions, nil
}

type importPathsPrompter struct {
	paths         map[string]string
	runtimeAction RuntimeFileAction
}

func (p importPathsPrompter) SelectImports(candidates []WorktreeCandidate) ([]ImportDecision, error) {
	decisions := []ImportDecision{}
	for _, candidate := range candidates {
		name, ok := p.paths[candidate.WorktreePath]
		if !ok {
			continue
		}
		decision := ImportDecision{
			WorktreePath: candidate.WorktreePath,
			Name:         name,
		}
		for _, file := range candidate.RuntimeFiles {
			decision.RuntimeFiles = append(decision.RuntimeFiles, RuntimeFileDecision{
				Path:   file.Path,
				Action: p.runtimeAction,
			})
		}
		decisions = append(decisions, decision)
	}
	return decisions, nil
}

func runtimeFilePaths(files []RuntimeFileCandidate) []string {
	paths := make([]string, 0, len(files))
	for _, file := range files {
		paths = append(paths, file.Path)
	}
	sort.Strings(paths)
	return paths
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
