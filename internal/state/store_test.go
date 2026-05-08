package state

import (
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestStoreOpenReopenAndUpsertProject(t *testing.T) {
	home := t.TempDir()
	store := openTestStore(t, home)

	project, err := store.UpsertProject(Project{
		Name:             "runtree",
		RepoCommonDir:    "/repos/runtree/.git",
		MainWorktreePath: "/repos/runtree",
	})
	if err != nil {
		t.Fatalf("UpsertProject() error = %v", err)
	}
	if project.ID == 0 {
		t.Fatal("UpsertProject() returned zero project ID")
	}
	if project.CreatedAt.IsZero() || project.UpdatedAt.IsZero() {
		t.Fatalf("UpsertProject() timestamps were not set: %+v", project)
	}

	if err := store.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	store = openTestStore(t, home)
	updated, err := store.UpsertProject(Project{
		Name:             "runtree-renamed",
		RepoCommonDir:    project.RepoCommonDir,
		MainWorktreePath: "/repos/runtree-renamed",
	})
	if err != nil {
		t.Fatalf("UpsertProject(update) error = %v", err)
	}
	if updated.ID != project.ID {
		t.Fatalf("UpsertProject(update) ID = %d, want %d", updated.ID, project.ID)
	}
	if updated.Name != "runtree-renamed" || updated.MainWorktreePath != "/repos/runtree-renamed" {
		t.Fatalf("UpsertProject(update) = %+v", updated)
	}

	found, err := store.ProjectByRepoCommonDir(project.RepoCommonDir)
	if err != nil {
		t.Fatalf("ProjectByRepoCommonDir() error = %v", err)
	}
	if found.Name != updated.Name || found.MainWorktreePath != updated.MainWorktreePath {
		t.Fatalf("ProjectByRepoCommonDir() = %+v, want %+v", found, updated)
	}
}

func TestStoreInstanceCRUD(t *testing.T) {
	store := openTestStore(t, t.TempDir())
	project := createTestProject(t, store, "runtree")
	started := time.Date(2026, 5, 8, 10, 0, 0, 123, time.UTC)
	stopped := time.Date(2026, 5, 8, 11, 0, 0, 456, time.UTC)
	exitCode := 42

	instance, err := store.CreateInstance(Instance{
		ProjectID:     project.ID,
		Name:          "api",
		Branch:        "feature/api",
		WorktreePath:  "/repos/runtree-api",
		Port:          9001,
		PID:           1234,
		Status:        StatusRunning,
		LogPath:       store.LogPath(project.Name, "api"),
		LastExitCode:  &exitCode,
		LastStartedAt: &started,
		LastStoppedAt: &stopped,
	})
	if err != nil {
		t.Fatalf("CreateInstance() error = %v", err)
	}
	if instance.ID == 0 {
		t.Fatal("CreateInstance() returned zero instance ID")
	}
	if instance.Source != SourceManual {
		t.Fatalf("CreateInstance() Source = %q, want %q", instance.Source, SourceManual)
	}
	if instance.Visibility != VisibilityVisible {
		t.Fatalf("CreateInstance() Visibility = %q, want %q", instance.Visibility, VisibilityVisible)
	}
	if instance.LastExitCode == nil || *instance.LastExitCode != exitCode {
		t.Fatalf("CreateInstance() LastExitCode = %v, want %d", instance.LastExitCode, exitCode)
	}
	if instance.LastStartedAt == nil || !instance.LastStartedAt.Equal(started) {
		t.Fatalf("CreateInstance() LastStartedAt = %v, want %s", instance.LastStartedAt, started)
	}
	if instance.LastStoppedAt == nil || !instance.LastStoppedAt.Equal(stopped) {
		t.Fatalf("CreateInstance() LastStoppedAt = %v, want %s", instance.LastStoppedAt, stopped)
	}

	byName, err := store.InstanceByName(project.ID, "api")
	if err != nil {
		t.Fatalf("InstanceByName() error = %v", err)
	}
	if byName.ID != instance.ID {
		t.Fatalf("InstanceByName() ID = %d, want %d", byName.ID, instance.ID)
	}
	byPath, err := store.InstanceByWorktreePath(project.ID, "/repos/runtree-api")
	if err != nil {
		t.Fatalf("InstanceByWorktreePath() error = %v", err)
	}
	if byPath.ID != instance.ID {
		t.Fatalf("InstanceByWorktreePath() ID = %d, want %d", byPath.ID, instance.ID)
	}

	restarted := time.Date(2026, 5, 8, 12, 0, 0, 789, time.UTC)
	instance.Name = "api-renamed"
	instance.Branch = "feature/api-renamed"
	instance.WorktreePath = "/repos/runtree-api-renamed"
	instance.Source = SourceCodex
	instance.Visibility = VisibilityIgnored
	instance.Port = 9002
	instance.PID = 0
	instance.Status = StatusStopped
	instance.LogPath = store.LogPath(project.Name, "api-renamed")
	instance.LastExitCode = nil
	instance.LastStartedAt = nil
	instance.LastStoppedAt = &restarted
	if err := store.UpdateInstance(instance); err != nil {
		t.Fatalf("UpdateInstance() error = %v", err)
	}

	updated, err := store.InstanceByName(project.ID, "api-renamed")
	if err != nil {
		t.Fatalf("InstanceByName(updated) error = %v", err)
	}
	if updated.Source != SourceCodex || updated.Visibility != VisibilityIgnored || updated.Port != 9002 {
		t.Fatalf("updated instance = %+v", updated)
	}
	if updated.LastExitCode != nil || updated.LastStartedAt != nil {
		t.Fatalf("updated nullable fields were not cleared: %+v", updated)
	}
	if updated.LastStoppedAt == nil || !updated.LastStoppedAt.Equal(restarted) {
		t.Fatalf("updated LastStoppedAt = %v, want %s", updated.LastStoppedAt, restarted)
	}

	if err := store.DeleteInstance(instance.ID); err != nil {
		t.Fatalf("DeleteInstance() error = %v", err)
	}
	if _, err := store.InstanceByName(project.ID, "api-renamed"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("InstanceByName(deleted) error = %v, want sql.ErrNoRows", err)
	}
}

func TestInstancesByProjectOrdersByName(t *testing.T) {
	store := openTestStore(t, t.TempDir())
	project := createTestProject(t, store, "runtree")

	for _, name := range []string{"worker", "api", "frontend"} {
		_, err := store.CreateInstance(Instance{
			ProjectID:    project.ID,
			Name:         name,
			Branch:       "feature/" + name,
			WorktreePath: "/repos/runtree-" + name,
			Port:         9000 + len(name),
			Status:       StatusStopped,
			LogPath:      store.LogPath(project.Name, name),
		})
		if err != nil {
			t.Fatalf("CreateInstance(%s) error = %v", name, err)
		}
	}

	instances, err := store.InstancesByProject(project.ID)
	if err != nil {
		t.Fatalf("InstancesByProject() error = %v", err)
	}
	got := make([]string, 0, len(instances))
	for _, instance := range instances {
		got = append(got, instance.Name)
	}
	want := []string{"api", "frontend", "worker"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("InstancesByProject() names = %v, want %v", got, want)
	}
}

func TestInstanceUniqueConstraintsAreScopedToProject(t *testing.T) {
	store := openTestStore(t, t.TempDir())
	project := createTestProject(t, store, "runtree")
	otherProject := createTestProject(t, store, "other")

	base := Instance{
		ProjectID:    project.ID,
		Name:         "api",
		Branch:       "feature/api",
		WorktreePath: "/repos/runtree-api",
		Port:         9001,
		Status:       StatusStopped,
		LogPath:      store.LogPath(project.Name, "api"),
	}
	if _, err := store.CreateInstance(base); err != nil {
		t.Fatalf("CreateInstance(base) error = %v", err)
	}

	tests := []struct {
		name     string
		instance Instance
	}{
		{
			name: "duplicate name",
			instance: Instance{
				ProjectID:    project.ID,
				Name:         base.Name,
				Branch:       "feature/other-name",
				WorktreePath: "/repos/runtree-other-name",
				Port:         9002,
				Status:       StatusStopped,
				LogPath:      store.LogPath(project.Name, "other-name"),
			},
		},
		{
			name: "duplicate path",
			instance: Instance{
				ProjectID:    project.ID,
				Name:         "other-path",
				Branch:       "feature/other-path",
				WorktreePath: base.WorktreePath,
				Port:         9003,
				Status:       StatusStopped,
				LogPath:      store.LogPath(project.Name, "other-path"),
			},
		},
		{
			name: "duplicate port",
			instance: Instance{
				ProjectID:    project.ID,
				Name:         "other-port",
				Branch:       "feature/other-port",
				WorktreePath: "/repos/runtree-other-port",
				Port:         base.Port,
				Status:       StatusStopped,
				LogPath:      store.LogPath(project.Name, "other-port"),
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := store.CreateInstance(tt.instance); err == nil {
				t.Fatal("CreateInstance() error = nil, want unique constraint error")
			}
		})
	}

	base.ProjectID = otherProject.ID
	if _, err := store.CreateInstance(base); err != nil {
		t.Fatalf("CreateInstance(same values in another project) error = %v", err)
	}
}

func TestProjectDeleteCascadesInstances(t *testing.T) {
	store := openTestStore(t, t.TempDir())
	project := createTestProject(t, store, "runtree")

	_, err := store.CreateInstance(Instance{
		ProjectID:    project.ID,
		Name:         "api",
		Branch:       "feature/api",
		WorktreePath: "/repos/runtree-api",
		Port:         9001,
		Status:       StatusStopped,
		LogPath:      store.LogPath(project.Name, "api"),
	})
	if err != nil {
		t.Fatalf("CreateInstance() error = %v", err)
	}

	if err := store.db.Delete(&projectRecord{}, project.ID).Error; err != nil {
		t.Fatalf("delete projectRecord error = %v", err)
	}
	instances, err := store.InstancesByProject(project.ID)
	if err != nil {
		t.Fatalf("InstancesByProject() error = %v", err)
	}
	if len(instances) != 0 {
		t.Fatalf("InstancesByProject() len = %d, want 0", len(instances))
	}
}

func TestOpenIncompatibleDatabaseErrorIncludesDeleteCommand(t *testing.T) {
	home := t.TempDir()
	dbPath := filepath.Join(home, "state.db")
	if err := os.WriteFile(dbPath, []byte("not a sqlite database"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	store, err := Open(home)
	if err == nil {
		_ = store.Close()
		t.Fatal("Open() error = nil, want incompatible database error")
	}
	if !strings.Contains(err.Error(), "rm "+shellQuote(dbPath)) {
		t.Fatalf("Open() error = %q, want delete command for %s", err, dbPath)
	}
}

func openTestStore(t *testing.T, home string) *Store {
	t.Helper()

	store, err := Open(home)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Fatalf("Close() error = %v", err)
		}
	})
	return store
}

func createTestProject(t *testing.T, store *Store, name string) Project {
	t.Helper()

	project, err := store.UpsertProject(Project{
		Name:             name,
		RepoCommonDir:    "/repos/" + name + "/.git",
		MainWorktreePath: "/repos/" + name,
	})
	if err != nil {
		t.Fatalf("UpsertProject(%s) error = %v", name, err)
	}
	return project
}
