package state

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
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
	db   *sql.DB
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
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open state db: %w", err)
	}

	store := &Store{db: db, home: home}
	if err := store.migrate(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return store, nil
}

func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
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
		result, err := s.db.Exec(`
			INSERT INTO projects (name, repo_common_dir, main_worktree_path, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?)
		`, project.Name, project.RepoCommonDir, project.MainWorktreePath, now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano))
		if err != nil {
			return Project{}, fmt.Errorf("insert project: %w", err)
		}
		id, err := result.LastInsertId()
		if err != nil {
			return Project{}, fmt.Errorf("project last insert id: %w", err)
		}
		project.ID = id
		project.CreatedAt = now
		project.UpdatedAt = now
		return project, nil
	}

	if _, err := s.db.Exec(`
		UPDATE projects
		SET name = ?, main_worktree_path = ?, updated_at = ?
		WHERE id = ?
	`, project.Name, project.MainWorktreePath, now.Format(time.RFC3339Nano), existing.ID); err != nil {
		return Project{}, fmt.Errorf("update project: %w", err)
	}

	existing.Name = project.Name
	existing.MainWorktreePath = project.MainWorktreePath
	existing.UpdatedAt = now
	return existing, nil
}

func (s *Store) ProjectByRepoCommonDir(repoCommonDir string) (Project, error) {
	row := s.db.QueryRow(`
		SELECT id, name, repo_common_dir, main_worktree_path, created_at, updated_at
		FROM projects
		WHERE repo_common_dir = ?
	`, repoCommonDir)
	return scanProject(row)
}

func (s *Store) CreateInstance(instance Instance) (Instance, error) {
	now := time.Now().UTC()
	if instance.Source == "" {
		instance.Source = SourceManual
	}
	if instance.Visibility == "" {
		instance.Visibility = VisibilityVisible
	}
	result, err := s.db.Exec(`
		INSERT INTO instances (
			project_id, name, branch, worktree_path, source, visibility, port, pid, status, log_path,
			last_exit_code, created_at, updated_at, last_started_at, last_stopped_at
		)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		instance.ProjectID,
		instance.Name,
		instance.Branch,
		instance.WorktreePath,
		instance.Source,
		instance.Visibility,
		instance.Port,
		instance.PID,
		instance.Status,
		instance.LogPath,
		toNullInt64(instance.LastExitCode),
		now.Format(time.RFC3339Nano),
		now.Format(time.RFC3339Nano),
		toNullTime(instance.LastStartedAt),
		toNullTime(instance.LastStoppedAt),
	)
	if err != nil {
		return Instance{}, fmt.Errorf("insert instance: %w", err)
	}

	id, err := result.LastInsertId()
	if err != nil {
		return Instance{}, fmt.Errorf("instance last insert id: %w", err)
	}
	instance.ID = id
	instance.CreatedAt = now
	instance.UpdatedAt = now
	return instance, nil
}

func (s *Store) UpdateInstance(instance Instance) error {
	now := time.Now().UTC()
	if instance.Source == "" {
		instance.Source = SourceManual
	}
	if instance.Visibility == "" {
		instance.Visibility = VisibilityVisible
	}
	_, err := s.db.Exec(`
		UPDATE instances
		SET name = ?, branch = ?, worktree_path = ?, source = ?, visibility = ?, port = ?, pid = ?, status = ?, log_path = ?,
		    last_exit_code = ?, updated_at = ?, last_started_at = ?, last_stopped_at = ?
		WHERE id = ?
	`,
		instance.Name,
		instance.Branch,
		instance.WorktreePath,
		instance.Source,
		instance.Visibility,
		instance.Port,
		instance.PID,
		instance.Status,
		instance.LogPath,
		toNullInt64(instance.LastExitCode),
		now.Format(time.RFC3339Nano),
		toNullTime(instance.LastStartedAt),
		toNullTime(instance.LastStoppedAt),
		instance.ID,
	)
	if err != nil {
		return fmt.Errorf("update instance %s: %w", instance.Name, err)
	}
	instance.UpdatedAt = now
	return nil
}

func (s *Store) InstancesByProject(projectID int64) ([]Instance, error) {
	rows, err := s.db.Query(`
		SELECT id, project_id, name, branch, worktree_path, source, visibility, port, pid, status, log_path,
		       last_exit_code, created_at, updated_at, last_started_at, last_stopped_at
		FROM instances
		WHERE project_id = ?
		ORDER BY name ASC
	`, projectID)
	if err != nil {
		return nil, fmt.Errorf("list instances: %w", err)
	}
	defer rows.Close()

	var instances []Instance
	for rows.Next() {
		instance, err := scanInstance(rows)
		if err != nil {
			return nil, err
		}
		instances = append(instances, instance)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate instances: %w", err)
	}
	return instances, nil
}

func (s *Store) InstanceByName(projectID int64, name string) (Instance, error) {
	row := s.db.QueryRow(`
		SELECT id, project_id, name, branch, worktree_path, source, visibility, port, pid, status, log_path,
		       last_exit_code, created_at, updated_at, last_started_at, last_stopped_at
		FROM instances
		WHERE project_id = ? AND name = ?
	`, projectID, name)
	return scanInstance(row)
}

func (s *Store) InstanceByWorktreePath(projectID int64, worktreePath string) (Instance, error) {
	row := s.db.QueryRow(`
		SELECT id, project_id, name, branch, worktree_path, source, visibility, port, pid, status, log_path,
		       last_exit_code, created_at, updated_at, last_started_at, last_stopped_at
		FROM instances
		WHERE project_id = ? AND worktree_path = ?
	`, projectID, worktreePath)
	return scanInstance(row)
}

func (s *Store) DeleteInstance(id int64) error {
	if _, err := s.db.Exec(`DELETE FROM instances WHERE id = ?`, id); err != nil {
		return fmt.Errorf("delete instance %d: %w", id, err)
	}
	return nil
}

func (s *Store) migrate() error {
	const schema = `
	PRAGMA foreign_keys = ON;

	CREATE TABLE IF NOT EXISTS projects (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT NOT NULL,
		repo_common_dir TEXT NOT NULL UNIQUE,
		main_worktree_path TEXT NOT NULL,
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL
	);

	CREATE TABLE IF NOT EXISTS instances (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		project_id INTEGER NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
		name TEXT NOT NULL,
		branch TEXT NOT NULL,
		worktree_path TEXT NOT NULL,
		source TEXT NOT NULL DEFAULT 'manual',
		visibility TEXT NOT NULL DEFAULT 'visible',
		port INTEGER NOT NULL,
		pid INTEGER NOT NULL DEFAULT 0,
		status TEXT NOT NULL,
		log_path TEXT NOT NULL,
		last_exit_code INTEGER NULL,
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL,
		last_started_at TEXT NULL,
		last_stopped_at TEXT NULL,
		UNIQUE(project_id, name),
		UNIQUE(project_id, worktree_path),
		UNIQUE(project_id, port)
	);
	`
	if _, err := s.db.Exec(schema); err != nil {
		return fmt.Errorf("migrate state db: %w", err)
	}
	if err := s.ensureInstanceColumn("source", "TEXT NOT NULL DEFAULT 'manual'"); err != nil {
		return err
	}
	if err := s.ensureInstanceColumn("visibility", "TEXT NOT NULL DEFAULT 'visible'"); err != nil {
		return err
	}
	return nil
}

type scanner interface {
	Scan(dest ...any) error
}

func scanProject(row scanner) (Project, error) {
	var project Project
	var createdAt string
	var updatedAt string
	if err := row.Scan(
		&project.ID,
		&project.Name,
		&project.RepoCommonDir,
		&project.MainWorktreePath,
		&createdAt,
		&updatedAt,
	); err != nil {
		return Project{}, err
	}

	created, err := time.Parse(time.RFC3339Nano, createdAt)
	if err != nil {
		return Project{}, fmt.Errorf("parse project created_at: %w", err)
	}
	updated, err := time.Parse(time.RFC3339Nano, updatedAt)
	if err != nil {
		return Project{}, fmt.Errorf("parse project updated_at: %w", err)
	}
	project.CreatedAt = created
	project.UpdatedAt = updated
	return project, nil
}

func scanInstance(row scanner) (Instance, error) {
	var instance Instance
	var createdAt string
	var updatedAt string
	var lastExit sql.NullInt64
	var lastStarted sql.NullString
	var lastStopped sql.NullString
	if err := row.Scan(
		&instance.ID,
		&instance.ProjectID,
		&instance.Name,
		&instance.Branch,
		&instance.WorktreePath,
		&instance.Source,
		&instance.Visibility,
		&instance.Port,
		&instance.PID,
		&instance.Status,
		&instance.LogPath,
		&lastExit,
		&createdAt,
		&updatedAt,
		&lastStarted,
		&lastStopped,
	); err != nil {
		return Instance{}, err
	}

	created, err := time.Parse(time.RFC3339Nano, createdAt)
	if err != nil {
		return Instance{}, fmt.Errorf("parse instance created_at: %w", err)
	}
	updated, err := time.Parse(time.RFC3339Nano, updatedAt)
	if err != nil {
		return Instance{}, fmt.Errorf("parse instance updated_at: %w", err)
	}
	instance.CreatedAt = created
	instance.UpdatedAt = updated
	if lastExit.Valid {
		exitCode := int(lastExit.Int64)
		instance.LastExitCode = &exitCode
	}
	if lastStarted.Valid {
		value, err := time.Parse(time.RFC3339Nano, lastStarted.String)
		if err != nil {
			return Instance{}, fmt.Errorf("parse instance last_started_at: %w", err)
		}
		instance.LastStartedAt = &value
	}
	if lastStopped.Valid {
		value, err := time.Parse(time.RFC3339Nano, lastStopped.String)
		if err != nil {
			return Instance{}, fmt.Errorf("parse instance last_stopped_at: %w", err)
		}
		instance.LastStoppedAt = &value
	}
	if instance.Source == "" {
		instance.Source = SourceManual
	}
	if instance.Visibility == "" {
		instance.Visibility = VisibilityVisible
	}
	return instance, nil
}

func toNullInt64(value *int) sql.NullInt64 {
	if value == nil {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: int64(*value), Valid: true}
}

func toNullTime(value *time.Time) sql.NullString {
	if value == nil {
		return sql.NullString{}
	}
	return sql.NullString{String: value.UTC().Format(time.RFC3339Nano), Valid: true}
}

func (s *Store) ensureInstanceColumn(name, definition string) error {
	rows, err := s.db.Query(`PRAGMA table_info(instances)`)
	if err != nil {
		return fmt.Errorf("inspect instances schema: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var cid int
		var columnName string
		var columnType string
		var notNull int
		var defaultValue sql.NullString
		var pk int
		if err := rows.Scan(&cid, &columnName, &columnType, &notNull, &defaultValue, &pk); err != nil {
			return fmt.Errorf("scan instances schema: %w", err)
		}
		if columnName == name {
			return nil
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate instances schema: %w", err)
	}

	query := fmt.Sprintf(`ALTER TABLE instances ADD COLUMN %s %s`, name, definition)
	if _, err := s.db.Exec(query); err != nil {
		return fmt.Errorf("add instances.%s column: %w", name, err)
	}
	return nil
}
