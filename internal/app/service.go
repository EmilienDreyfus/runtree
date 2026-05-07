package app

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
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

type ScanCandidate struct {
	Branch        string
	WorktreePath  string
	SuggestedName string
	ReservedNames []string
	Port          int
}

type ScanResult struct {
	Candidates    []ScanCandidate
	Imported      []state.Instance
	Updated       []state.Instance
	Missing       []state.Instance
	DiscoveryOnly bool
}

type PruneResult struct {
	Pruned         []state.Instance
	SkippedRunning []state.Instance
}

type ScanPrompter interface {
	ReviewCandidate(candidate ScanCandidate) (approved bool, name string, err error)
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

func (s Service) ScanProject(startDir string, interactive bool, prompter ScanPrompter) (ScanResult, error) {
	ctx, store, err := s.loadProject(startDir)
	if err != nil {
		return ScanResult{}, err
	}
	defer store.Close()

	instances, err := s.reconcileInstances(store, ctx.Project.ID)
	if err != nil {
		return ScanResult{}, err
	}

	worktrees, err := gitutil.ListWorktrees(ctx.ConfigDir)
	if err != nil {
		return ScanResult{}, err
	}

	byPath := make(map[string]state.Instance, len(instances))
	usedNames := make(map[string]bool, len(instances))
	reservedPorts := make(map[int]bool, len(instances))
	for _, instance := range instances {
		byPath[instance.WorktreePath] = instance
		usedNames[instance.Name] = true
		reservedPorts[instance.Port] = true
	}

	result := ScanResult{DiscoveryOnly: !interactive}
	seenPaths := make(map[string]bool, len(worktrees))
	for _, worktree := range worktrees {
		seenPaths[worktree.Path] = true

		if instance, ok := byPath[worktree.Path]; ok {
			changed := false
			source, visibility := classifyWorktree(worktree)
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
			if worktree.Main && ctx.Project.MainWorktreePath != worktree.Path {
				ctx.Project.MainWorktreePath = worktree.Path
				if _, err := store.UpsertProject(ctx.Project); err != nil {
					return ScanResult{}, err
				}
			}
			if changed {
				if err := store.UpdateInstance(instance); err != nil {
					return ScanResult{}, err
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
			return ScanResult{}, err
		}
		reservedPorts[port] = true

		suggestedName := uniqueName(defaultName(worktree.Branch, worktree.Path), usedNames)
		candidate := ScanCandidate{
			Branch:        worktree.Branch,
			WorktreePath:  worktree.Path,
			SuggestedName: suggestedName,
			ReservedNames: sortedNames(usedNames),
			Port:          port,
		}
		result.Candidates = append(result.Candidates, candidate)
		if !interactive {
			continue
		}
		if prompter == nil {
			return ScanResult{}, errors.New("interactive scan requires a prompter")
		}

		approved, chosenName, err := prompter.ReviewCandidate(candidate)
		if err != nil {
			return ScanResult{}, err
		}
		if !approved {
			continue
		}

		name := strings.TrimSpace(chosenName)
		if name == "" {
			name = suggestedName
		}
		if usedNames[name] {
			return ScanResult{}, fmt.Errorf("instance name %q already exists", name)
		}

		instance := state.Instance{
			ProjectID:    ctx.Project.ID,
			Name:         name,
			Branch:       worktree.Branch,
			WorktreePath: worktree.Path,
			Source:       source,
			Visibility:   visibility,
			Port:         port,
			PID:          0,
			Status:       state.StatusStopped,
			LogPath:      store.LogPath(ctx.Project.Name, name),
		}
		if err := store.EnsureLogDir(ctx.Project.Name); err != nil {
			return ScanResult{}, fmt.Errorf("ensure logs directory: %w", err)
		}
		instance, err = store.CreateInstance(instance)
		if err != nil {
			return ScanResult{}, err
		}
		usedNames[name] = true
		byPath[instance.WorktreePath] = instance
		result.Imported = append(result.Imported, instance)
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
				return ScanResult{}, err
			}
			result.Missing = append(result.Missing, instance)
		}
	}

	return result, nil
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
		if instance.Visibility != state.VisibilityIgnored {
			continue
		}
		if instance.Status == state.StatusRunning {
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
