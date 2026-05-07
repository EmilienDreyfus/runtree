package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	FileName            = ".runtree.yaml"
	DefaultPortStart    = 8100
	DefaultPortEnd      = 8199
	DefaultWebURLFormat = "http://127.0.0.1:{port}"
)

type Config struct {
	Version int           `yaml:"version"`
	Project ProjectConfig `yaml:"project"`
	Run     RunConfig     `yaml:"run"`
	Ports   PortsConfig   `yaml:"ports"`
	Web     WebConfig     `yaml:"web,omitempty"`
}

type ProjectConfig struct {
	Name string `yaml:"name"`
}

type RunConfig struct {
	Command string `yaml:"command"`
}

type PortsConfig struct {
	Start int `yaml:"start"`
	End   int `yaml:"end"`
}

type WebConfig struct {
	URLTemplate string `yaml:"url_template,omitempty"`
}

func Default() *Config {
	return &Config{
		Version: 1,
		Ports: PortsConfig{
			Start: DefaultPortStart,
			End:   DefaultPortEnd,
		},
		Web: WebConfig{
			URLTemplate: DefaultWebURLFormat,
		},
	}
}

func Find(startDir string) (string, error) {
	current, err := filepath.Abs(startDir)
	if err != nil {
		return "", fmt.Errorf("resolve start directory: %w", err)
	}

	for {
		candidate := filepath.Join(current, FileName)
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		} else if !errors.Is(err, os.ErrNotExist) {
			return "", fmt.Errorf("stat %s: %w", candidate, err)
		}

		parent := filepath.Dir(current)
		if parent == current {
			return "", fmt.Errorf("%s not found from %s", FileName, startDir)
		}
		current = parent
	}
}

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}

	cfg := Default()
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

func Write(path string, cfg *Config) error {
	if cfg == nil {
		return errors.New("config is required")
	}
	if err := cfg.Validate(); err != nil {
		return err
	}

	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}

	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write config: %w", err)
	}
	return nil
}

func (c *Config) Validate() error {
	if c == nil {
		return errors.New("config is required")
	}
	if c.Version != 1 {
		return fmt.Errorf("unsupported config version %d", c.Version)
	}
	if strings.TrimSpace(c.Project.Name) == "" {
		return errors.New("project.name is required")
	}
	if strings.TrimSpace(c.Run.Command) == "" {
		return errors.New("run.command is required")
	}
	if !strings.Contains(c.Run.Command, "{port}") {
		return errors.New("run.command must contain {port}")
	}
	if c.Ports.Start <= 0 || c.Ports.End <= 0 {
		return errors.New("ports.start and ports.end must be positive")
	}
	if c.Ports.Start > c.Ports.End {
		return errors.New("ports.start must be less than or equal to ports.end")
	}
	return nil
}

func (c *Config) RenderRunCommand(port int) string {
	return strings.ReplaceAll(c.Run.Command, "{port}", fmt.Sprintf("%d", port))
}

func (c *Config) RenderWebURL(port int) string {
	template := c.Web.URLTemplate
	if strings.TrimSpace(template) == "" {
		template = DefaultWebURLFormat
	}
	return strings.ReplaceAll(template, "{port}", fmt.Sprintf("%d", port))
}
