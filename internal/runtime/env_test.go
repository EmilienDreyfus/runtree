package runtime

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestIsolatedEnvDropsActiveRuntimeState(t *testing.T) {
	t.Parallel()

	venv := filepath.Join("tmp", "repo-a", ".venv")
	conda := filepath.Join("opt", "conda", "envs", "api")
	path := strings.Join([]string{
		filepath.Join(venv, "bin"),
		"/opt/homebrew/bin",
		filepath.Join(conda, "bin"),
		"/usr/bin",
	}, string(filepath.ListSeparator))

	got := IsolatedEnv([]string{
		"HOME=/Users/alice",
		"SECRET_KEY=dev-secret",
		"VIRTUAL_ENV=" + venv,
		"POETRY_ACTIVE=1",
		"CONDA_PREFIX=" + conda,
		"PYENV_VERSION=3.13.5",
		"npm_lifecycle_event=dev",
		"npm_package_name=api",
		"PATH=" + path,
	})

	if hasEnvKey(got, "VIRTUAL_ENV") || hasEnvKey(got, "POETRY_ACTIVE") || hasEnvKey(got, "PYENV_VERSION") {
		t.Fatalf("IsolatedEnv() kept active Python env vars: %v", got)
	}
	if hasEnvKey(got, "npm_lifecycle_event") || hasEnvKey(got, "npm_package_name") {
		t.Fatalf("IsolatedEnv() kept npm lifecycle vars: %v", got)
	}
	if !hasEnvEntry(got, "SECRET_KEY=dev-secret") {
		t.Fatalf("IsolatedEnv() dropped app env var SECRET_KEY: %v", got)
	}

	gotPath := envValue(t, got, "PATH")
	if strings.Contains(gotPath, filepath.Join(venv, "bin")) || strings.Contains(gotPath, filepath.Join(conda, "bin")) {
		t.Fatalf("PATH = %q, want active env bins removed", gotPath)
	}
	if !strings.Contains(gotPath, "/opt/homebrew/bin") || !strings.Contains(gotPath, "/usr/bin") {
		t.Fatalf("PATH = %q, want normal toolchain paths preserved", gotPath)
	}
}

func TestInstanceEnvAddsRuntimeVariables(t *testing.T) {
	t.Parallel()

	got := InstanceEnv([]string{"HOME=/Users/alice"}, "Project", "studio", 8103)
	for _, want := range []string{
		"PORT=8103",
		"RUNTREE_PORT=8103",
		"RUNTREE_INSTANCE=studio",
		"RUNTREE_PROJECT=Project",
	} {
		if !hasEnvEntry(got, want) {
			t.Fatalf("InstanceEnv() missing %q in %v", want, got)
		}
	}
}

func TestIsolatedEnvKeepsPathWhenNoActiveEnvExists(t *testing.T) {
	t.Parallel()

	path := strings.Join([]string{"bin", "/usr/bin"}, string(filepath.ListSeparator))
	got := IsolatedEnv([]string{"PATH=" + path})

	if gotPath := envValue(t, got, "PATH"); gotPath != path {
		t.Fatalf("PATH = %q, want %q", gotPath, path)
	}
}

func hasEnvKey(env []string, key string) bool {
	prefix := key + "="
	for _, entry := range env {
		if strings.HasPrefix(entry, prefix) {
			return true
		}
	}
	return false
}

func hasEnvEntry(env []string, want string) bool {
	for _, entry := range env {
		if entry == want {
			return true
		}
	}
	return false
}

func envValue(t *testing.T, env []string, key string) string {
	t.Helper()
	prefix := key + "="
	for _, entry := range env {
		if strings.HasPrefix(entry, prefix) {
			return strings.TrimPrefix(entry, prefix)
		}
	}
	t.Fatalf("missing %s in %v", key, env)
	return ""
}
