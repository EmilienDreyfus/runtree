package runtime

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"
)

type StartRequest struct {
	Command      string
	WorktreePath string
	LogPath      string
	Project      string
	Instance     string
	Port         int
}

type Manager struct {
	GracePeriod  time.Duration
	StartupProbe time.Duration
}

func NewManager() Manager {
	return Manager{
		GracePeriod:  5 * time.Second,
		StartupProbe: 750 * time.Millisecond,
	}
}

func (m Manager) Start(req StartRequest) (int, error) {
	if strings.TrimSpace(req.Command) == "" {
		return 0, errors.New("command is required")
	}
	if strings.TrimSpace(req.WorktreePath) == "" {
		return 0, errors.New("worktree path is required")
	}
	if strings.TrimSpace(req.LogPath) == "" {
		return 0, errors.New("log path is required")
	}

	logFile, err := os.OpenFile(req.LogPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return 0, fmt.Errorf("open log file: %w", err)
	}
	defer func() {
		_ = logFile.Close()
	}()

	devNull, err := os.Open(os.DevNull)
	if err != nil {
		return 0, fmt.Errorf("open %s: %w", os.DevNull, err)
	}
	defer func() {
		_ = devNull.Close()
	}()

	cmd := exec.Command("/bin/sh", "-lc", req.Command)
	cmd.Dir = req.WorktreePath
	cmd.Env = append(os.Environ(),
		fmt.Sprintf("PORT=%d", req.Port),
		fmt.Sprintf("RUNTREE_PORT=%d", req.Port),
		fmt.Sprintf("RUNTREE_INSTANCE=%s", req.Instance),
		fmt.Sprintf("RUNTREE_PROJECT=%s", req.Project),
	)
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.Stdin = devNull
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}

	if err := cmd.Start(); err != nil {
		return 0, fmt.Errorf("start command: %w", err)
	}
	pid := cmd.Process.Pid

	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()

	select {
	case err := <-done:
		if err != nil {
			return 0, fmt.Errorf("process exited during startup: %w", err)
		}
		return 0, errors.New("process exited during startup")
	case <-time.After(m.startupProbe()):
		return pid, nil
	}
}

func (m Manager) Stop(pid int) error {
	if pid <= 0 {
		return nil
	}

	if !IsProcessAlive(pid) {
		return nil
	}

	if err := signalGroup(pid, syscall.SIGTERM); err != nil && !errors.Is(err, syscall.ESRCH) {
		return fmt.Errorf("terminate process group %d: %w", pid, err)
	}

	deadline := time.Now().Add(m.GracePeriod)
	for time.Now().Before(deadline) {
		if !IsProcessAlive(pid) {
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}

	if err := signalGroup(pid, syscall.SIGKILL); err != nil && !errors.Is(err, syscall.ESRCH) {
		return fmt.Errorf("kill process group %d: %w", pid, err)
	}
	return nil
}

func IsProcessAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}

func signalGroup(pid int, signal syscall.Signal) error {
	if pid <= 0 {
		return nil
	}
	if err := syscall.Kill(-pid, signal); err == nil || errors.Is(err, syscall.ESRCH) {
		return err
	}
	return syscall.Kill(pid, signal)
}

func (m Manager) startupProbe() time.Duration {
	if m.StartupProbe > 0 {
		return m.StartupProbe
	}
	return 750 * time.Millisecond
}
