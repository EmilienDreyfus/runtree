package settings

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

const fileName = "settings.yaml"

type Settings struct {
	EditorCommand string `yaml:"editor_command,omitempty"`
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

func Load(home string) (Settings, error) {
	if home == "" {
		var err error
		home, err = DefaultHomeDir()
		if err != nil {
			return Settings{}, err
		}
	}

	path := filepath.Join(home, fileName)
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return Settings{}, nil
	}
	if err != nil {
		return Settings{}, fmt.Errorf("read settings: %w", err)
	}

	var settings Settings
	if err := yaml.Unmarshal(data, &settings); err != nil {
		return Settings{}, fmt.Errorf("parse settings: %w", err)
	}
	return settings, settings.Validate()
}

func Save(home string, settings Settings) error {
	if home == "" {
		var err error
		home, err = DefaultHomeDir()
		if err != nil {
			return err
		}
	}
	if err := settings.Validate(); err != nil {
		return err
	}
	if err := os.MkdirAll(home, 0o755); err != nil {
		return fmt.Errorf("create settings home: %w", err)
	}

	data, err := yaml.Marshal(settings)
	if err != nil {
		return fmt.Errorf("marshal settings: %w", err)
	}

	path := filepath.Join(home, fileName)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write settings: %w", err)
	}
	return nil
}

func (s Settings) Validate() error {
	if strings.TrimSpace(s.EditorCommand) == "" {
		return nil
	}
	if !strings.Contains(s.EditorCommand, "{path}") {
		return errors.New("editor_command must contain {path}")
	}
	return nil
}
