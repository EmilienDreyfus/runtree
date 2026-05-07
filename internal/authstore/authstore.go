package authstore

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

const fileName = "auth.yaml"

type Auth struct {
	BaseURL       string `yaml:"base_url,omitempty"`
	AccessToken   string `yaml:"access_token,omitempty"`
	AccountHandle string `yaml:"account_handle,omitempty"`
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

func Load(home string) (Auth, error) {
	if home == "" {
		var err error
		home, err = DefaultHomeDir()
		if err != nil {
			return Auth{}, err
		}
	}

	path := filepath.Join(home, fileName)
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return Auth{}, nil
	}
	if err != nil {
		return Auth{}, fmt.Errorf("read auth: %w", err)
	}

	var auth Auth
	if err := yaml.Unmarshal(data, &auth); err != nil {
		return Auth{}, fmt.Errorf("parse auth: %w", err)
	}
	return auth, auth.Validate()
}

func Save(home string, auth Auth) error {
	if home == "" {
		var err error
		home, err = DefaultHomeDir()
		if err != nil {
			return err
		}
	}
	if err := auth.Validate(); err != nil {
		return err
	}
	if err := os.MkdirAll(home, 0o755); err != nil {
		return fmt.Errorf("create auth home: %w", err)
	}

	data, err := yaml.Marshal(auth)
	if err != nil {
		return fmt.Errorf("marshal auth: %w", err)
	}

	path := filepath.Join(home, fileName)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("write auth: %w", err)
	}
	return nil
}

func Clear(home string) error {
	if home == "" {
		var err error
		home, err = DefaultHomeDir()
		if err != nil {
			return err
		}
	}

	path := filepath.Join(home, fileName)
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove auth: %w", err)
	}
	return nil
}

func (a Auth) Validate() error {
	if strings.TrimSpace(a.AccessToken) == "" {
		if strings.TrimSpace(a.BaseURL) != "" || strings.TrimSpace(a.AccountHandle) != "" {
			return errors.New("access_token is required when auth data is present")
		}
		return nil
	}
	if strings.TrimSpace(a.BaseURL) == "" {
		return errors.New("base_url is required")
	}
	return nil
}
