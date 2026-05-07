package gitutil

import (
	"bufio"
	"bytes"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
)

type RepoInfo struct {
	TopLevel  string
	CommonDir string
}

type Worktree struct {
	Path     string
	Branch   string
	Head     string
	Detached bool
	Bare     bool
	Main     bool
}

func ResolveRepo(startDir string) (RepoInfo, error) {
	topLevelBytes, err := execGit(startDir, "rev-parse", "--show-toplevel")
	if err != nil {
		return RepoInfo{}, fmt.Errorf("resolve git top-level: %w", err)
	}
	topLevel := strings.TrimSpace(string(topLevelBytes))
	if topLevel == "" {
		return RepoInfo{}, fmt.Errorf("git top-level not found from %s", startDir)
	}

	commonDirBytes, err := execGit(topLevel, "rev-parse", "--git-common-dir")
	if err != nil {
		return RepoInfo{}, fmt.Errorf("resolve git common dir: %w", err)
	}
	commonDir := strings.TrimSpace(string(commonDirBytes))
	if commonDir == "" {
		return RepoInfo{}, fmt.Errorf("git common dir not found from %s", topLevel)
	}
	if !filepath.IsAbs(commonDir) {
		commonDir = filepath.Join(topLevel, commonDir)
	}

	topLevel, err = CanonicalPath(topLevel)
	if err != nil {
		return RepoInfo{}, err
	}
	commonDir, err = CanonicalPath(commonDir)
	if err != nil {
		return RepoInfo{}, err
	}

	return RepoInfo{
		TopLevel:  topLevel,
		CommonDir: commonDir,
	}, nil
}

func ListWorktrees(startDir string) ([]Worktree, error) {
	repo, err := ResolveRepo(startDir)
	if err != nil {
		return nil, err
	}

	output, err := execGit(repo.TopLevel, "worktree", "list", "--porcelain")
	if err != nil {
		return nil, fmt.Errorf("list git worktrees: %w", err)
	}
	return ParseWorktreeList(output)
}

func ParseWorktreeList(data []byte) ([]Worktree, error) {
	var worktrees []Worktree
	current := Worktree{}
	seenCurrent := false

	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		line := scanner.Text()
		if strings.TrimSpace(line) == "" {
			if seenCurrent {
				current.Main = len(worktrees) == 0
				worktrees = append(worktrees, current)
				current = Worktree{}
				seenCurrent = false
			}
			continue
		}

		parts := strings.SplitN(line, " ", 2)
		key := parts[0]
		value := ""
		if len(parts) == 2 {
			value = parts[1]
		}

		switch key {
		case "worktree":
			current.Path = value
			seenCurrent = true
		case "HEAD":
			current.Head = value
		case "branch":
			current.Branch = strings.TrimPrefix(value, "refs/heads/")
		case "detached":
			current.Detached = true
		case "bare":
			current.Bare = true
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan git worktree output: %w", err)
	}
	if seenCurrent {
		current.Main = len(worktrees) == 0
		worktrees = append(worktrees, current)
	}

	for i := range worktrees {
		canonical, err := CanonicalPath(worktrees[i].Path)
		if err != nil {
			return nil, err
		}
		worktrees[i].Path = canonical
	}

	return worktrees, nil
}

func CanonicalPath(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolve absolute path for %s: %w", path, err)
	}

	resolved, err := filepath.EvalSymlinks(abs)
	if err == nil {
		return filepath.Clean(resolved), nil
	}
	return filepath.Clean(abs), nil
}

func execGit(dir string, args ...string) ([]byte, error) {
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("%v: %w: %s", cmd.Args, err, strings.TrimSpace(string(output)))
	}
	return output, nil
}
