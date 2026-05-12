package app

import (
	"database/sql"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/EmilienDreyfus/runtree/internal/config"
	"github.com/EmilienDreyfus/runtree/internal/gitutil"
	"github.com/EmilienDreyfus/runtree/internal/ports"
	"github.com/EmilienDreyfus/runtree/internal/runtime"
	"github.com/EmilienDreyfus/runtree/internal/state"
)

type Service struct {
	HomeDir     string
	Runtime     runtime.Manager
	PortChecker func(int) bool
}

const AllInstancesTarget = "all"

func IsReservedInstanceName(name string) bool {
	return strings.EqualFold(strings.TrimSpace(name), AllInstancesTarget)
}

type ProjectContext struct {
	ConfigPath string
	ConfigDir  string
	Config     *config.Config
	Repo       gitutil.RepoInfo
	Project    state.Project
}

type InitInput struct {
	Name           string
	RunCommand     string
	PortStart      int
	PortEnd        int
	WebURLTemplate string
}

type WorktreeCandidate struct {
	Branch        string
	WorktreePath  string
	SuggestedName string
	ReservedNames []string
	Port          int
	RuntimeFiles  []RuntimeFileCandidate
	source        string
	visibility    string
}

type ImportDecision struct {
	WorktreePath string
	Name         string
	RuntimeFiles []RuntimeFileDecision
}

type InventoryResult struct {
	Instances          []state.Instance
	Candidates         []WorktreeCandidate
	RuntimeFileTargets []RuntimeFileTarget
	Imported           []state.Instance
	Updated            []state.Instance
	Missing            []state.Instance
}

type PruneResult struct {
	Pruned         []state.Instance
	SkippedRunning []state.Instance
}

type RunCommandInput struct {
	Args   []string
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer
}

type RuntimeFileAction string

const (
	RuntimeFileSkip    RuntimeFileAction = "skip"
	RuntimeFileSymlink RuntimeFileAction = "symlink"
	RuntimeFileCopy    RuntimeFileAction = "copy"
)

type RuntimeFileCandidate struct {
	Path       string
	SourcePath string
	TargetPath string
}

type RuntimeFileDecision struct {
	Path   string
	Action RuntimeFileAction
}

type RuntimeFileTarget struct {
	InstanceName string
	WorktreePath string
	RuntimeFiles []RuntimeFileCandidate
}

type RuntimeFileTargetDecision struct {
	WorktreePath string
	RuntimeFiles []RuntimeFileDecision
}

type ImportPrompter interface {
	SelectImports(candidates []WorktreeCandidate) ([]ImportDecision, error)
}

func NewService(homeDir string) Service {
	return Service{
		HomeDir:     homeDir,
		Runtime:     runtime.NewManager(),
		PortChecker: ports.IsAvailable,
	}
}

func (s Service) InitProject(startDir string, input InitInput) (ProjectContext, error) {
	repo, err := gitutil.ResolveRepo(startDir)
	if err != nil {
		return ProjectContext{}, err
	}

	configPath := filepath.Join(repo.TopLevel, config.FileName)
	if _, err := os.Stat(configPath); err == nil {
		return ProjectContext{}, fmt.Errorf("%s already exists", configPath)
	} else if !errors.Is(err, os.ErrNotExist) {
		return ProjectContext{}, fmt.Errorf("stat %s: %w", configPath, err)
	}

	cfg := config.Default()
	cfg.Project.Name = strings.TrimSpace(input.Name)
	cfg.Run.Command = strings.TrimSpace(input.RunCommand)
	if input.PortStart != 0 {
		cfg.Ports.Start = input.PortStart
	}
	if input.PortEnd != 0 {
		cfg.Ports.End = input.PortEnd
	}
	if strings.TrimSpace(input.WebURLTemplate) != "" {
		cfg.Web.URLTemplate = strings.TrimSpace(input.WebURLTemplate)
	}

	if err := config.Write(configPath, cfg); err != nil {
		return ProjectContext{}, err
	}

	store, err := state.Open(s.HomeDir)
	if err != nil {
		return ProjectContext{}, err
	}
	defer store.Close()

	project, err := store.UpsertProject(state.Project{
		Name:             cfg.Project.Name,
		RepoCommonDir:    repo.CommonDir,
		MainWorktreePath: repo.TopLevel,
	})
	if err != nil {
		return ProjectContext{}, err
	}

	return ProjectContext{
		ConfigPath: configPath,
		ConfigDir:  repo.TopLevel,
		Config:     cfg,
		Repo:       repo,
		Project:    project,
	}, nil
}

func (s Service) Inventory(startDir string, includeIgnored bool, importNew bool, prompter ImportPrompter) (InventoryResult, error) {
	ctx, store, err := s.loadProject(startDir)
	if err != nil {
		return InventoryResult{}, err
	}
	defer store.Close()

	instances, err := s.reconcileInstances(store, ctx.Project.ID)
	if err != nil {
		return InventoryResult{}, err
	}

	worktrees, err := gitutil.ListWorktrees(ctx.ConfigDir)
	if err != nil {
		return InventoryResult{}, err
	}
	if mainWorktreePath := mainWorktreePath(worktrees); mainWorktreePath != "" && ctx.Project.MainWorktreePath != mainWorktreePath {
		ctx.Project.MainWorktreePath = mainWorktreePath
		if _, err := store.UpsertProject(ctx.Project); err != nil {
			return InventoryResult{}, err
		}
	}
	runtimeFiles := discoverRuntimeFiles(ctx.Project.MainWorktreePath)

	byPath := make(map[string]state.Instance, len(instances))
	usedNames := reservedInstanceNameSet()
	reservedPorts := make(map[int]bool, len(instances))
	for _, instance := range instances {
		byPath[instance.WorktreePath] = instance
		usedNames[instance.Name] = true
		reservedPorts[instance.Port] = true
	}

	result := InventoryResult{}
	seenPaths := make(map[string]bool, len(worktrees))
	for _, worktree := range worktrees {
		seenPaths[worktree.Path] = true

		if instance, ok := byPath[worktree.Path]; ok {
			changed := false
			worktreeExists := pathExists(worktree.Path)
			source, visibility := classifyWorktree(worktree)
			if !worktreeExists {
				visibility = visibilityForKnownMissingWorktree(worktree, source)
			}
			if instance.Branch != worktree.Branch {
				instance.Branch = worktree.Branch
				changed = true
			}
			if instance.WorktreePath != worktree.Path {
				instance.WorktreePath = worktree.Path
				changed = true
			}
			if instance.Source != source {
				instance.Source = source
				changed = true
			}
			if instance.Visibility != visibility {
				instance.Visibility = visibility
				changed = true
			}
			if changed {
				if err := store.UpdateInstance(instance); err != nil {
					return InventoryResult{}, err
				}
				result.Updated = append(result.Updated, instance)
			}
			continue
		}
		source, visibility := classifyWorktree(worktree)
		if visibility == state.VisibilityIgnored {
			continue
		}

		port, err := ports.AllocateWithChecker(ctx.Config.Ports.Start, ctx.Config.Ports.End, reservedPorts, s.portChecker())
		if err != nil {
			return InventoryResult{}, err
		}
		reservedPorts[port] = true

		suggestedName := uniqueName(defaultName(worktree.Branch, worktree.Path), usedNames)
		candidate := WorktreeCandidate{
			Branch:        worktree.Branch,
			WorktreePath:  worktree.Path,
			SuggestedName: suggestedName,
			ReservedNames: sortedNames(usedNames),
			Port:          port,
			RuntimeFiles:  missingRuntimeFiles(worktree.Path, runtimeFiles),
			source:        source,
			visibility:    visibility,
		}
		result.Candidates = append(result.Candidates, candidate)
		usedNames[suggestedName] = true
	}

	for _, instance := range instances {
		if seenPaths[instance.WorktreePath] {
			continue
		}
		if instance.Status != state.StatusMissing {
			instance.Status = state.StatusMissing
			if !runtime.IsProcessAlive(instance.PID) {
				instance.PID = 0
			}
			if err := store.UpdateInstance(instance); err != nil {
				return InventoryResult{}, err
			}
			result.Missing = append(result.Missing, instance)
		}
	}

	if importNew && len(result.Candidates) > 0 {
		if prompter == nil {
			return InventoryResult{}, errors.New("worktree import requires a prompter")
		}
		decisions, err := prompter.SelectImports(result.Candidates)
		if err != nil {
			return InventoryResult{}, err
		}
		imported, err := s.importWorktrees(store, ctx.Project, result.Candidates, instances, decisions)
		if err != nil {
			return InventoryResult{}, err
		}
		result.Imported = imported
	}

	instances, err = store.InstancesByProject(ctx.Project.ID)
	if err != nil {
		return InventoryResult{}, err
	}
	result.Instances = filterInstances(instances, includeIgnored)
	excludedRuntimeTargets := make(map[string]bool, len(result.Imported))
	for _, instance := range result.Imported {
		excludedRuntimeTargets[instance.WorktreePath] = true
	}
	result.RuntimeFileTargets = runtimeFileTargets(result.Instances, runtimeFiles, excludedRuntimeTargets)
	return result, nil
}

func (s Service) importWorktrees(store *state.Store, project state.Project, candidates []WorktreeCandidate, instances []state.Instance, decisions []ImportDecision) ([]state.Instance, error) {
	if len(decisions) == 0 {
		return nil, nil
	}

	byPath := make(map[string]WorktreeCandidate, len(candidates))
	for _, candidate := range candidates {
		byPath[candidate.WorktreePath] = candidate
	}

	usedNames := reservedInstanceNameSet()
	for _, instance := range instances {
		usedNames[instance.Name] = true
	}

	if err := store.EnsureLogDir(project.Name); err != nil {
		return nil, fmt.Errorf("ensure logs directory: %w", err)
	}

	imported := make([]state.Instance, 0, len(decisions))
	importedPaths := make(map[string]bool, len(decisions))
	for _, decision := range decisions {
		candidate, ok := byPath[decision.WorktreePath]
		if !ok {
			return nil, fmt.Errorf("unknown worktree candidate %s", decision.WorktreePath)
		}
		if importedPaths[decision.WorktreePath] {
			return nil, fmt.Errorf("worktree candidate %s selected more than once", decision.WorktreePath)
		}
		importedPaths[decision.WorktreePath] = true

		name := strings.TrimSpace(decision.Name)
		if name == "" {
			name = candidate.SuggestedName
		}
		if name == "" {
			return nil, errors.New("instance name is required")
		}
		if IsReservedInstanceName(name) {
			return nil, fmt.Errorf("instance name %q is reserved", name)
		}
		if usedNames[name] {
			return nil, fmt.Errorf("instance name %q already exists", name)
		}
		if err := applyRuntimeFileDecisions(candidate, decision.RuntimeFiles); err != nil {
			return nil, err
		}

		instance := state.Instance{
			ProjectID:    project.ID,
			Name:         name,
			Branch:       candidate.Branch,
			WorktreePath: candidate.WorktreePath,
			Source:       candidate.source,
			Visibility:   candidate.visibility,
			Port:         candidate.Port,
			PID:          0,
			Status:       state.StatusStopped,
			LogPath:      store.LogPath(project.Name, name),
		}
		created, err := store.CreateInstance(instance)
		if err != nil {
			return nil, err
		}
		usedNames[name] = true
		imported = append(imported, created)
	}
	return imported, nil
}

func (s Service) ListInstances(startDir string, includeIgnored bool) ([]state.Instance, error) {
	ctx, store, err := s.loadProject(startDir)
	if err != nil {
		return nil, err
	}
	defer store.Close()
	instances, err := s.reconcileInstances(store, ctx.Project.ID)
	if err != nil {
		return nil, err
	}
	return filterInstances(instances, includeIgnored), nil
}

func (s Service) StartInstance(startDir, name string) (state.Instance, error) {
	ctx, store, err := s.loadProject(startDir)
	if err != nil {
		return state.Instance{}, err
	}
	defer store.Close()

	if _, err := s.reconcileInstances(store, ctx.Project.ID); err != nil {
		return state.Instance{}, err
	}

	instance, err := store.InstanceByName(ctx.Project.ID, name)
	if err != nil {
		return state.Instance{}, fmt.Errorf("load instance %s: %w", name, err)
	}
	if instance.Status == state.StatusMissing {
		return state.Instance{}, fmt.Errorf("instance %s is missing its worktree", name)
	}
	if runtime.IsProcessAlive(instance.PID) {
		return state.Instance{}, fmt.Errorf("instance %s is already running", name)
	}
	if !s.portChecker()(instance.Port) {
		return state.Instance{}, fmt.Errorf("port %d is already in use", instance.Port)
	}
	if err := store.EnsureLogDir(ctx.Project.Name); err != nil {
		return state.Instance{}, fmt.Errorf("ensure logs directory: %w", err)
	}

	pid, err := s.Runtime.Start(runtime.StartRequest{
		Command:      ctx.Config.RenderRunCommand(instance.Port),
		WorktreePath: instance.WorktreePath,
		LogPath:      instance.LogPath,
		Project:      ctx.Project.Name,
		Instance:     instance.Name,
		Port:         instance.Port,
	})
	if err != nil {
		logTail := tailFile(instance.LogPath, 8, 4096)
		if logTail != "" {
			return state.Instance{}, fmt.Errorf("%w:\n%s", err, logTail)
		}
		return state.Instance{}, err
	}

	now := time.Now().UTC()
	instance.PID = pid
	instance.Status = state.StatusRunning
	instance.LastStartedAt = &now
	instance.LastExitCode = nil
	if err := store.UpdateInstance(instance); err != nil {
		return state.Instance{}, err
	}
	return instance, nil
}

func (s Service) StopInstance(startDir, name string) (state.Instance, error) {
	ctx, store, err := s.loadProject(startDir)
	if err != nil {
		return state.Instance{}, err
	}
	defer store.Close()

	if _, err := s.reconcileInstances(store, ctx.Project.ID); err != nil {
		return state.Instance{}, err
	}

	instance, err := store.InstanceByName(ctx.Project.ID, name)
	if err != nil {
		return state.Instance{}, fmt.Errorf("load instance %s: %w", name, err)
	}

	if err := s.Runtime.Stop(instance.PID); err != nil {
		return state.Instance{}, err
	}

	now := time.Now().UTC()
	instance.LastStoppedAt = &now
	if !pathExists(instance.WorktreePath) {
		instance.Status = state.StatusMissing
	} else {
		instance.Status = state.StatusStopped
	}
	instance.PID = 0
	if err := store.UpdateInstance(instance); err != nil {
		return state.Instance{}, err
	}
	return instance, nil
}

func (s Service) RestartInstance(startDir, name string) (state.Instance, error) {
	instance, err := s.StopInstance(startDir, name)
	if err != nil {
		return state.Instance{}, err
	}
	if instance.Status == state.StatusMissing {
		return state.Instance{}, fmt.Errorf("instance %s is missing its worktree", name)
	}
	return s.StartInstance(startDir, name)
}

func (s Service) InstanceDetails(startDir, name string) (ProjectContext, state.Instance, error) {
	ctx, store, err := s.loadProject(startDir)
	if err != nil {
		return ProjectContext{}, state.Instance{}, err
	}
	defer store.Close()

	if _, err := s.reconcileInstances(store, ctx.Project.ID); err != nil {
		return ProjectContext{}, state.Instance{}, err
	}
	instance, err := store.InstanceByName(ctx.Project.ID, name)
	if err != nil {
		return ProjectContext{}, state.Instance{}, fmt.Errorf("load instance %s: %w", name, err)
	}
	return ctx, instance, nil
}

func (s Service) RunInInstance(startDir, name string, input RunCommandInput) error {
	if len(input.Args) == 0 {
		return errors.New("command is required")
	}
	ctx, instance, err := s.InstanceDetails(startDir, name)
	if err != nil {
		return err
	}
	if instance.Status == state.StatusMissing {
		return fmt.Errorf("instance %s is missing its worktree", instance.Name)
	}

	cmd := exec.Command(input.Args[0], input.Args[1:]...)
	cmd.Dir = instance.WorktreePath
	cmd.Env = runtime.InstanceEnv(os.Environ(), ctx.Project.Name, instance.Name, instance.Port)
	cmd.Stdin = input.Stdin
	cmd.Stdout = input.Stdout
	cmd.Stderr = input.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("run %s: %w", input.Args[0], err)
	}
	return nil
}

func (s Service) ConfigureRuntimeFiles(startDir string, decisions []RuntimeFileTargetDecision) ([]RuntimeFileTarget, error) {
	if len(decisions) == 0 {
		return nil, nil
	}
	ctx, store, err := s.loadProject(startDir)
	if err != nil {
		return nil, err
	}
	defer store.Close()

	instances, err := s.reconcileInstances(store, ctx.Project.ID)
	if err != nil {
		return nil, err
	}
	worktrees, err := gitutil.ListWorktrees(ctx.ConfigDir)
	if err != nil {
		return nil, err
	}
	if mainWorktreePath := mainWorktreePath(worktrees); mainWorktreePath != "" {
		ctx.Project.MainWorktreePath = mainWorktreePath
	}
	runtimeFiles := discoverRuntimeFiles(ctx.Project.MainWorktreePath)
	targets := runtimeFileTargets(instances, runtimeFiles, nil)

	targetsByPath := make(map[string]RuntimeFileTarget, len(targets))
	for _, target := range targets {
		targetsByPath[target.WorktreePath] = target
	}

	configured := make([]RuntimeFileTarget, 0, len(decisions))
	for _, decision := range decisions {
		target, ok := targetsByPath[decision.WorktreePath]
		if !ok {
			return nil, fmt.Errorf("unknown runtime file target %s", decision.WorktreePath)
		}
		candidate := WorktreeCandidate{
			WorktreePath: target.WorktreePath,
			RuntimeFiles: target.RuntimeFiles,
		}
		if err := applyRuntimeFileDecisions(candidate, decision.RuntimeFiles); err != nil {
			return nil, err
		}
		configured = append(configured, target)
	}
	return configured, nil
}

func (s Service) PruneInstances(startDir string) (PruneResult, error) {
	ctx, store, err := s.loadProject(startDir)
	if err != nil {
		return PruneResult{}, err
	}
	defer store.Close()

	instances, err := s.reconcileInstances(store, ctx.Project.ID)
	if err != nil {
		return PruneResult{}, err
	}

	result := PruneResult{}
	for _, instance := range instances {
		if !isPrunableInstance(instance) {
			continue
		}
		if instance.Status == state.StatusRunning || runtime.IsProcessAlive(instance.PID) {
			result.SkippedRunning = append(result.SkippedRunning, instance)
			continue
		}
		if err := store.DeleteInstance(instance.ID); err != nil {
			return PruneResult{}, err
		}
		result.Pruned = append(result.Pruned, instance)
	}
	return result, nil
}

func isPrunableInstance(instance state.Instance) bool {
	return instance.Status == state.StatusMissing || instance.Visibility == state.VisibilityIgnored
}

func mainWorktreePath(worktrees []gitutil.Worktree) string {
	for _, worktree := range worktrees {
		if worktree.Main {
			return worktree.Path
		}
	}
	return ""
}

func discoverRuntimeFiles(mainWorktreePath string) []RuntimeFileCandidate {
	if strings.TrimSpace(mainWorktreePath) == "" {
		return nil
	}

	matches, err := filepath.Glob(filepath.Join(mainWorktreePath, ".env*"))
	if err != nil {
		return nil
	}
	envrc := filepath.Join(mainWorktreePath, ".envrc")
	if _, err := os.Stat(envrc); err == nil {
		matches = append(matches, envrc)
	}
	sort.Strings(matches)

	files := make([]RuntimeFileCandidate, 0, len(matches))
	seen := map[string]bool{}
	for _, sourcePath := range matches {
		rel, err := filepath.Rel(mainWorktreePath, sourcePath)
		if err != nil || !isRuntimeFileName(rel) || seen[rel] {
			continue
		}
		if !isIgnoredFile(mainWorktreePath, rel) {
			continue
		}
		info, err := os.Stat(sourcePath)
		if err != nil || info.IsDir() {
			continue
		}
		seen[rel] = true
		files = append(files, RuntimeFileCandidate{
			Path:       rel,
			SourcePath: sourcePath,
		})
	}
	return files
}

func isRuntimeFileName(path string) bool {
	base := filepath.Base(path)
	if path != base {
		return false
	}
	lowerBase := strings.ToLower(base)
	if lowerBase == ".envrc" {
		return true
	}
	if !strings.HasPrefix(lowerBase, ".env") {
		return false
	}
	if strings.Contains(lowerBase, "example") || strings.Contains(lowerBase, "sample") || strings.Contains(lowerBase, "template") {
		return false
	}
	return true
}

func isIgnoredFile(worktreePath, relPath string) bool {
	cmd := exec.Command("git", "-C", worktreePath, "check-ignore", "-q", "--", relPath)
	return cmd.Run() == nil
}

func missingRuntimeFiles(worktreePath string, files []RuntimeFileCandidate) []RuntimeFileCandidate {
	if len(files) == 0 {
		return nil
	}

	missing := make([]RuntimeFileCandidate, 0, len(files))
	for _, file := range files {
		targetPath := filepath.Join(worktreePath, file.Path)
		if _, err := os.Lstat(targetPath); err == nil {
			continue
		} else if !errors.Is(err, os.ErrNotExist) {
			continue
		}
		file.TargetPath = targetPath
		missing = append(missing, file)
	}
	return missing
}

func runtimeFileTargets(instances []state.Instance, files []RuntimeFileCandidate, excluded map[string]bool) []RuntimeFileTarget {
	if len(files) == 0 {
		return nil
	}

	targets := make([]RuntimeFileTarget, 0, len(instances))
	for _, instance := range instances {
		if instance.Status == state.StatusMissing || excluded[instance.WorktreePath] {
			continue
		}
		missing := missingRuntimeFiles(instance.WorktreePath, files)
		if len(missing) == 0 {
			continue
		}
		targets = append(targets, RuntimeFileTarget{
			InstanceName: instance.Name,
			WorktreePath: instance.WorktreePath,
			RuntimeFiles: missing,
		})
	}
	return targets
}

func applyRuntimeFileDecisions(candidate WorktreeCandidate, decisions []RuntimeFileDecision) error {
	if len(decisions) == 0 {
		return nil
	}

	filesByPath := make(map[string]RuntimeFileCandidate, len(candidate.RuntimeFiles))
	for _, file := range candidate.RuntimeFiles {
		filesByPath[file.Path] = file
	}
	for _, decision := range decisions {
		if decision.Action == "" || decision.Action == RuntimeFileSkip {
			continue
		}
		file, ok := filesByPath[decision.Path]
		if !ok {
			return fmt.Errorf("unknown runtime file %s for worktree %s", decision.Path, candidate.WorktreePath)
		}
		if err := applyRuntimeFileDecision(file, decision.Action); err != nil {
			return err
		}
	}
	return nil
}

func applyRuntimeFileDecision(file RuntimeFileCandidate, action RuntimeFileAction) error {
	if err := validateRuntimeFileCandidate(file); err != nil {
		return err
	}
	if _, err := os.Lstat(file.TargetPath); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("stat runtime file target %s: %w", file.TargetPath, err)
	}
	if err := os.MkdirAll(filepath.Dir(file.TargetPath), 0o755); err != nil {
		return fmt.Errorf("create runtime file directory: %w", err)
	}

	switch action {
	case RuntimeFileSymlink:
		if err := os.Symlink(file.SourcePath, file.TargetPath); err != nil {
			return fmt.Errorf("link runtime file %s: %w", file.Path, err)
		}
	case RuntimeFileCopy:
		if err := copyRuntimeFile(file.SourcePath, file.TargetPath); err != nil {
			return fmt.Errorf("copy runtime file %s: %w", file.Path, err)
		}
	default:
		return fmt.Errorf("unsupported runtime file action %q", action)
	}
	return nil
}

func validateRuntimeFileCandidate(file RuntimeFileCandidate) error {
	if strings.TrimSpace(file.Path) == "" {
		return errors.New("runtime file path is required")
	}
	if filepath.IsAbs(file.Path) || filepath.Clean(file.Path) != file.Path || strings.HasPrefix(file.Path, "..") {
		return fmt.Errorf("invalid runtime file path %s", file.Path)
	}
	if strings.TrimSpace(file.SourcePath) == "" || strings.TrimSpace(file.TargetPath) == "" {
		return fmt.Errorf("runtime file %s has invalid source or target", file.Path)
	}
	return nil
}

func copyRuntimeFile(sourcePath, targetPath string) error {
	source, err := os.Open(sourcePath)
	if err != nil {
		return err
	}
	defer source.Close()

	info, err := source.Stat()
	if err != nil {
		return err
	}
	target, err := os.OpenFile(targetPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, info.Mode().Perm())
	if err != nil {
		return err
	}
	defer target.Close()

	_, err = io.Copy(target, source)
	return err
}

func (s Service) loadProject(startDir string) (ProjectContext, *state.Store, error) {
	configPath, err := config.Find(startDir)
	if err != nil {
		return ProjectContext{}, nil, err
	}
	cfg, err := config.Load(configPath)
	if err != nil {
		return ProjectContext{}, nil, err
	}
	configDir := filepath.Dir(configPath)

	repo, err := gitutil.ResolveRepo(configDir)
	if err != nil {
		return ProjectContext{}, nil, err
	}

	store, err := state.Open(s.HomeDir)
	if err != nil {
		return ProjectContext{}, nil, err
	}

	project, err := store.UpsertProject(state.Project{
		Name:             cfg.Project.Name,
		RepoCommonDir:    repo.CommonDir,
		MainWorktreePath: repo.TopLevel,
	})
	if err != nil {
		_ = store.Close()
		return ProjectContext{}, nil, err
	}

	return ProjectContext{
		ConfigPath: configPath,
		ConfigDir:  configDir,
		Config:     cfg,
		Repo:       repo,
		Project:    project,
	}, store, nil
}

func (s Service) reconcileInstances(store *state.Store, projectID int64) ([]state.Instance, error) {
	instances, err := store.InstancesByProject(projectID)
	if err != nil {
		return nil, err
	}

	for i := range instances {
		instance := &instances[i]
		alive := runtime.IsProcessAlive(instance.PID)
		exists := pathExists(instance.WorktreePath)

		desiredStatus := state.StatusStopped
		switch {
		case !exists:
			desiredStatus = state.StatusMissing
			if !alive {
				instance.PID = 0
			}
		case alive:
			desiredStatus = state.StatusRunning
		default:
			desiredStatus = state.StatusStopped
			instance.PID = 0
		}

		if instance.Status != desiredStatus {
			instance.Status = desiredStatus
		}
		if err := store.UpdateInstance(*instance); err != nil {
			return nil, err
		}
	}

	return store.InstancesByProject(projectID)
}

func defaultName(branch, worktreePath string) string {
	branch = strings.TrimSpace(branch)
	if branch != "" {
		parts := strings.Split(branch, "/")
		branch = parts[len(parts)-1]
	}
	if branch == "" {
		branch = filepath.Base(worktreePath)
	}
	return sanitizeName(branch)
}

func sanitizeName(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "instance"
	}
	replacer := strings.NewReplacer(" ", "-", "_", "-", "/", "-", "\\", "-")
	value = replacer.Replace(strings.ToLower(value))
	value = strings.Trim(value, "-")
	if value == "" {
		return "instance"
	}
	return value
}

func uniqueName(base string, used map[string]bool) string {
	if !used[base] {
		return base
	}
	for idx := 2; ; idx++ {
		candidate := fmt.Sprintf("%s-%d", base, idx)
		if !used[candidate] {
			return candidate
		}
	}
}

func reservedInstanceNameSet() map[string]bool {
	return map[string]bool{AllInstancesTarget: true}
}

func sortedNames(used map[string]bool) []string {
	names := make([]string, 0, len(used))
	for name := range used {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func pathExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func IsNotFound(err error) bool {
	return errors.Is(err, sql.ErrNoRows)
}

func (s Service) portChecker() func(int) bool {
	if s.PortChecker != nil {
		return s.PortChecker
	}
	return ports.IsAvailable
}

func classifyWorktree(worktree gitutil.Worktree) (string, string) {
	source := classifyWorktreeSource(worktree)
	if worktree.Bare || !pathExists(worktree.Path) {
		return source, state.VisibilityIgnored
	}
	if source == state.SourceCursor && worktree.Detached {
		return source, state.VisibilityIgnored
	}
	if source == state.SourceClaude && worktree.Detached {
		return source, state.VisibilityIgnored
	}
	return source, state.VisibilityVisible
}

func visibilityForKnownMissingWorktree(worktree gitutil.Worktree, source string) string {
	if worktree.Bare {
		return state.VisibilityIgnored
	}
	if source == state.SourceCursor && worktree.Detached {
		return state.VisibilityIgnored
	}
	if source == state.SourceClaude && worktree.Detached {
		return state.VisibilityIgnored
	}
	return state.VisibilityVisible
}

func classifyWorktreeSource(worktree gitutil.Worktree) string {
	clean := filepath.Clean(worktree.Path)
	switch {
	case worktree.Main:
		return state.SourceMain
	case hasPathFragment(clean, filepath.Join(".codex", "worktrees")):
		return state.SourceCodex
	case hasPathFragment(clean, filepath.Join(".cursor", "worktrees")):
		return state.SourceCursor
	case hasPathFragment(clean, filepath.Join(".claude", "worktrees")):
		return state.SourceClaude
	default:
		return state.SourceManual
	}
}

func hasPathFragment(path, fragment string) bool {
	needle := string(os.PathSeparator) + filepath.Clean(fragment) + string(os.PathSeparator)
	return strings.Contains(path+string(os.PathSeparator), needle)
}

func filterInstances(instances []state.Instance, includeIgnored bool) []state.Instance {
	if includeIgnored {
		return instances
	}

	filtered := make([]state.Instance, 0, len(instances))
	for _, instance := range instances {
		if instance.Visibility == state.VisibilityIgnored {
			continue
		}
		filtered = append(filtered, instance)
	}
	return filtered
}

func tailFile(path string, maxLines, maxBytes int) string {
	data, err := os.ReadFile(path)
	if err != nil || len(data) == 0 {
		return ""
	}
	if maxBytes > 0 && len(data) > maxBytes {
		data = data[len(data)-maxBytes:]
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) > maxLines {
		lines = lines[len(lines)-maxLines:]
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}
