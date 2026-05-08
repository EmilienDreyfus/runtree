package state

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

const (
	StatusRunning = "running"
	StatusStopped = "stopped"
	StatusMissing = "missing"

	SourceMain   = "main"
	SourceManual = "manual"
	SourceCodex  = "codex"
	SourceCursor = "cursor"
	SourceClaude = "claude"

	VisibilityVisible = "visible"
	VisibilityIgnored = "ignored"
)

type Store struct {
	db   *gorm.DB
	home string
}

type Project struct {
	ID               int64
	Name             string
	RepoCommonDir    string
	MainWorktreePath string
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

type Instance struct {
	ID            int64
	ProjectID     int64
	Name          string
	Branch        string
	WorktreePath  string
	Source        string
	Visibility    string
	Port          int
	PID           int
	Status        string
	LogPath       string
	LastExitCode  *int
	CreatedAt     time.Time
	UpdatedAt     time.Time
	LastStartedAt *time.Time
	LastStoppedAt *time.Time
}

type projectRecord struct {
	ID               int64     `gorm:"primaryKey;autoIncrement"`
	Name             string    `gorm:"not null"`
	RepoCommonDir    string    `gorm:"column:repo_common_dir;not null;uniqueIndex"`
	MainWorktreePath string    `gorm:"column:main_worktree_path;not null"`
	CreatedAt        time.Time `gorm:"column:created_at;not null"`
	UpdatedAt        time.Time `gorm:"column:updated_at;not null"`
}

func (projectRecord) TableName() string {
	return "projects"
}

type instanceRecord struct {
	ID            int64          `gorm:"primaryKey;autoIncrement"`
	ProjectID     int64          `gorm:"column:project_id;not null;uniqueIndex:idx_instances_project_name;uniqueIndex:idx_instances_project_worktree_path;uniqueIndex:idx_instances_project_port"`
	Name          string         `gorm:"not null;uniqueIndex:idx_instances_project_name"`
	Branch        string         `gorm:"not null"`
	WorktreePath  string         `gorm:"column:worktree_path;not null;uniqueIndex:idx_instances_project_worktree_path"`
	Source        string         `gorm:"not null;default:manual"`
	Visibility    string         `gorm:"not null;default:visible"`
	Port          int            `gorm:"not null;uniqueIndex:idx_instances_project_port"`
	PID           int            `gorm:"column:pid;not null;default:0"`
	Status        string         `gorm:"not null"`
	LogPath       string         `gorm:"column:log_path;not null"`
	LastExitCode  *int           `gorm:"column:last_exit_code"`
	CreatedAt     time.Time      `gorm:"column:created_at;not null"`
	UpdatedAt     time.Time      `gorm:"column:updated_at;not null"`
	LastStartedAt *time.Time     `gorm:"column:last_started_at"`
	LastStoppedAt *time.Time     `gorm:"column:last_stopped_at"`
	Project       *projectRecord `gorm:"foreignKey:ProjectID;references:ID;constraint:OnDelete:CASCADE"`
}

func (instanceRecord) TableName() string {
	return "instances"
}

func DefaultHomeDir() (string, error) {
	if override := os.Getenv("RUNTREE_HOME"); override != "" {
		return filepath.Abs(override)
	}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve user home: %w", err)
	}
	return filepath.Join(homeDir, ".runtree"), nil
}

func Open(home string) (*Store, error) {
	if home == "" {
		var err error
		home, err = DefaultHomeDir()
		if err != nil {
			return nil, err
		}
	}
	if err := os.MkdirAll(home, 0o755); err != nil {
		return nil, fmt.Errorf("create state home: %w", err)
	}

	dbPath := filepath.Join(home, "state.db")
	db, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
		NowFunc: func() time.Time {
			return time.Now().UTC()
		},
	})
	if err != nil {
		return nil, stateDBError("open state db", dbPath, err)
	}

	sqlDB, err := db.DB()
	if err != nil {
		return nil, stateDBError("access state db", dbPath, err)
	}
	sqlDB.SetMaxOpenConns(1)

	if err := db.Exec("PRAGMA foreign_keys = ON").Error; err != nil {
		_ = sqlDB.Close()
		return nil, stateDBError("enable state db foreign keys", dbPath, err)
	}

	store := &Store{db: db, home: home}
	if err := store.migrate(); err != nil {
		_ = sqlDB.Close()
		return nil, stateDBError("migrate state db", dbPath, err)
	}
	return store, nil
}

func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	sqlDB, err := s.db.DB()
	if err != nil {
		return err
	}
	return sqlDB.Close()
}

func (s *Store) LogsDir(projectName string) string {
	return filepath.Join(s.home, "logs", projectName)
}

func (s *Store) LogPath(projectName, instanceName string) string {
	return filepath.Join(s.LogsDir(projectName), instanceName+".log")
}

func (s *Store) EnsureLogDir(projectName string) error {
	return os.MkdirAll(s.LogsDir(projectName), 0o755)
}

func (s *Store) UpsertProject(project Project) (Project, error) {
	now := time.Now().UTC()
	existing, err := s.ProjectByRepoCommonDir(project.RepoCommonDir)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return Project{}, err
	}
	if errors.Is(err, sql.ErrNoRows) {
		record := projectToRecord(project)
		record.CreatedAt = now
		record.UpdatedAt = now
		if err := s.db.Create(&record).Error; err != nil {
			return Project{}, fmt.Errorf("insert project: %w", err)
		}
		return recordToProject(record), nil
	}

	updates := map[string]any{
		"name":               project.Name,
		"main_worktree_path": project.MainWorktreePath,
		"updated_at":         now,
	}
	if err := s.db.Model(&projectRecord{}).Where("id = ?", existing.ID).Updates(updates).Error; err != nil {
		return Project{}, fmt.Errorf("update project: %w", err)
	}

	existing.Name = project.Name
	existing.MainWorktreePath = project.MainWorktreePath
	existing.UpdatedAt = now
	return existing, nil
}

func (s *Store) ProjectByRepoCommonDir(repoCommonDir string) (Project, error) {
	var record projectRecord
	if err := normalizeNotFound(s.db.Where("repo_common_dir = ?", repoCommonDir).First(&record).Error); err != nil {
		return Project{}, err
	}
	return recordToProject(record), nil
}

func (s *Store) CreateInstance(instance Instance) (Instance, error) {
	now := time.Now().UTC()
	if instance.Source == "" {
		instance.Source = SourceManual
	}
	if instance.Visibility == "" {
		instance.Visibility = VisibilityVisible
	}

	record := instanceToRecord(instance)
	record.CreatedAt = now
	record.UpdatedAt = now
	if err := s.db.Create(&record).Error; err != nil {
		return Instance{}, fmt.Errorf("insert instance: %w", err)
	}
	return recordToInstance(record), nil
}

func (s *Store) UpdateInstance(instance Instance) error {
	now := time.Now().UTC()
	if instance.Source == "" {
		instance.Source = SourceManual
	}
	if instance.Visibility == "" {
		instance.Visibility = VisibilityVisible
	}

	updates := map[string]any{
		"name":            instance.Name,
		"branch":          instance.Branch,
		"worktree_path":   instance.WorktreePath,
		"source":          instance.Source,
		"visibility":      instance.Visibility,
		"port":            instance.Port,
		"pid":             instance.PID,
		"status":          instance.Status,
		"log_path":        instance.LogPath,
		"last_exit_code":  instance.LastExitCode,
		"updated_at":      now,
		"last_started_at": instance.LastStartedAt,
		"last_stopped_at": instance.LastStoppedAt,
	}
	if err := s.db.Model(&instanceRecord{}).Where("id = ?", instance.ID).Updates(updates).Error; err != nil {
		return fmt.Errorf("update instance %s: %w", instance.Name, err)
	}
	return nil
}

func (s *Store) InstancesByProject(projectID int64) ([]Instance, error) {
	var records []instanceRecord
	if err := s.db.Where("project_id = ?", projectID).Order("name ASC").Find(&records).Error; err != nil {
		return nil, fmt.Errorf("list instances: %w", err)
	}

	instances := make([]Instance, 0, len(records))
	for _, record := range records {
		instances = append(instances, recordToInstance(record))
	}
	return instances, nil
}

func (s *Store) InstanceByName(projectID int64, name string) (Instance, error) {
	var record instanceRecord
	err := s.db.Where("project_id = ? AND name = ?", projectID, name).First(&record).Error
	if err := normalizeNotFound(err); err != nil {
		return Instance{}, err
	}
	return recordToInstance(record), nil
}

func (s *Store) InstanceByWorktreePath(projectID int64, worktreePath string) (Instance, error) {
	var record instanceRecord
	err := s.db.Where("project_id = ? AND worktree_path = ?", projectID, worktreePath).First(&record).Error
	if err := normalizeNotFound(err); err != nil {
		return Instance{}, err
	}
	return recordToInstance(record), nil
}

func (s *Store) DeleteInstance(id int64) error {
	if err := s.db.Delete(&instanceRecord{}, id).Error; err != nil {
		return fmt.Errorf("delete instance %d: %w", id, err)
	}
	return nil
}

func (s *Store) migrate() error {
	return s.db.AutoMigrate(&projectRecord{}, &instanceRecord{})
}

func projectToRecord(project Project) projectRecord {
	return projectRecord{
		ID:               project.ID,
		Name:             project.Name,
		RepoCommonDir:    project.RepoCommonDir,
		MainWorktreePath: project.MainWorktreePath,
		CreatedAt:        project.CreatedAt,
		UpdatedAt:        project.UpdatedAt,
	}
}

func recordToProject(record projectRecord) Project {
	return Project{
		ID:               record.ID,
		Name:             record.Name,
		RepoCommonDir:    record.RepoCommonDir,
		MainWorktreePath: record.MainWorktreePath,
		CreatedAt:        record.CreatedAt,
		UpdatedAt:        record.UpdatedAt,
	}
}

func instanceToRecord(instance Instance) instanceRecord {
	return instanceRecord{
		ID:            instance.ID,
		ProjectID:     instance.ProjectID,
		Name:          instance.Name,
		Branch:        instance.Branch,
		WorktreePath:  instance.WorktreePath,
		Source:        instance.Source,
		Visibility:    instance.Visibility,
		Port:          instance.Port,
		PID:           instance.PID,
		Status:        instance.Status,
		LogPath:       instance.LogPath,
		LastExitCode:  instance.LastExitCode,
		CreatedAt:     instance.CreatedAt,
		UpdatedAt:     instance.UpdatedAt,
		LastStartedAt: instance.LastStartedAt,
		LastStoppedAt: instance.LastStoppedAt,
	}
}

func recordToInstance(record instanceRecord) Instance {
	instance := Instance{
		ID:            record.ID,
		ProjectID:     record.ProjectID,
		Name:          record.Name,
		Branch:        record.Branch,
		WorktreePath:  record.WorktreePath,
		Source:        record.Source,
		Visibility:    record.Visibility,
		Port:          record.Port,
		PID:           record.PID,
		Status:        record.Status,
		LogPath:       record.LogPath,
		LastExitCode:  record.LastExitCode,
		CreatedAt:     record.CreatedAt,
		UpdatedAt:     record.UpdatedAt,
		LastStartedAt: record.LastStartedAt,
		LastStoppedAt: record.LastStoppedAt,
	}
	if instance.Source == "" {
		instance.Source = SourceManual
	}
	if instance.Visibility == "" {
		instance.Visibility = VisibilityVisible
	}
	return instance
}

func normalizeNotFound(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return sql.ErrNoRows
	}
	return err
}

func stateDBError(action, dbPath string, err error) error {
	return fmt.Errorf("%s: %w. If the local state database is incompatible, delete it and retry: rm %s", action, err, shellQuote(dbPath))
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}
