package runtime

import (
	"fmt"
	"path/filepath"
	"strings"
)

var isolatedEnvDropKeys = map[string]struct{}{
	"VIRTUAL_ENV":           {},
	"VIRTUAL_ENV_PROMPT":    {},
	"POETRY_ACTIVE":         {},
	"PIPENV_ACTIVE":         {},
	"PIPENV_PIPFILE":        {},
	"CONDA_DEFAULT_ENV":     {},
	"CONDA_PREFIX":          {},
	"CONDA_PROMPT_MODIFIER": {},
	"PYENV_VERSION":         {},
	"PYTHONHOME":            {},
	"__PYVENV_LAUNCHER__":   {},
	"BUNDLE_GEMFILE":        {},
	"BUNDLER_ORIG_MANPATH":  {},
	"BUNDLER_ORIG_PATH":     {},
	"BUNDLER_VERSION":       {},
	"GEM_HOME":              {},
	"GEM_PATH":              {},
	"RUBYLIB":               {},
}

var isolatedEnvDropPrefixes = []string{
	"CONDA_",
	"npm_lifecycle_",
	"npm_package_",
}

// IsolatedEnv keeps the user's normal environment, but removes activation state
// from the shell that invoked runtree. Commands still get app-level variables
// such as SECRET_KEY or DATABASE_URL when the user exports them deliberately.
func IsolatedEnv(base []string) []string {
	values := envMap(base)
	activePathRoots := activeEnvPathRoots(values)

	env := make([]string, 0, len(base))
	for _, entry := range base {
		key, value, ok := strings.Cut(entry, "=")
		if !ok {
			env = append(env, entry)
			continue
		}
		if shouldDropEnvKey(key) {
			continue
		}
		if key == "PATH" {
			value = cleanPath(value, activePathRoots)
			if value == "" {
				continue
			}
			entry = key + "=" + value
		}
		env = append(env, entry)
	}
	return env
}

func activeEnvPathRoots(values map[string]string) []string {
	roots := make([]string, 0, 3)
	for _, key := range []string{"VIRTUAL_ENV", "CONDA_PREFIX"} {
		if values[key] != "" {
			roots = append(roots, filepath.Join(values[key], "bin"))
		}
	}
	if values["GEM_HOME"] != "" {
		roots = append(roots, values["GEM_HOME"])
	}
	return roots
}

func InstanceEnv(base []string, project, instance string, port int) []string {
	env := IsolatedEnv(base)
	return append(env,
		fmt.Sprintf("PORT=%d", port),
		fmt.Sprintf("RUNTREE_PORT=%d", port),
		fmt.Sprintf("RUNTREE_INSTANCE=%s", instance),
		fmt.Sprintf("RUNTREE_PROJECT=%s", project),
	)
}

func envMap(env []string) map[string]string {
	values := make(map[string]string, len(env))
	for _, entry := range env {
		key, value, ok := strings.Cut(entry, "=")
		if ok {
			values[key] = value
		}
	}
	return values
}

func shouldDropEnvKey(key string) bool {
	if _, ok := isolatedEnvDropKeys[key]; ok {
		return true
	}
	for _, prefix := range isolatedEnvDropPrefixes {
		if strings.HasPrefix(key, prefix) {
			return true
		}
	}
	return false
}

func cleanPath(pathValue string, roots []string) string {
	if strings.TrimSpace(pathValue) == "" {
		return pathValue
	}
	parts := strings.Split(pathValue, string(filepath.ListSeparator))
	kept := parts[:0]
	for _, part := range parts {
		if isActivePath(part, roots) {
			continue
		}
		kept = append(kept, part)
	}
	return strings.Join(kept, string(filepath.ListSeparator))
}

func isActivePath(path string, roots []string) bool {
	if path == "" {
		return false
	}
	cleanPath := filepath.Clean(path)
	for _, root := range roots {
		if root == "" || root == "." {
			continue
		}
		cleanRoot := filepath.Clean(root)
		if cleanPath == cleanRoot {
			return true
		}
	}
	return false
}
