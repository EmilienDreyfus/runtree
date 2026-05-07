package openers

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

type ExecSpec struct {
	Name     string
	Args     []string
	UseShell bool
}

type EditorPreset struct {
	ID            string
	Label         string
	MacAppNames   []string
	LinuxCommands []string
}

var editorPresets = []EditorPreset{
	{ID: "cursor", Label: "Cursor", MacAppNames: []string{"Cursor.app"}, LinuxCommands: []string{"cursor"}},
	{ID: "codex", Label: "Codex", MacAppNames: []string{"Codex.app"}},
	{ID: "vscode", Label: "VS Code", MacAppNames: []string{"Visual Studio Code.app"}, LinuxCommands: []string{"code"}},
	{ID: "zed", Label: "Zed", MacAppNames: []string{"Zed.app"}, LinuxCommands: []string{"zed"}},
	{ID: "windsurf", Label: "Windsurf", MacAppNames: []string{"Windsurf.app"}, LinuxCommands: []string{"windsurf"}},
	{ID: "pycharm", Label: "PyCharm", MacAppNames: []string{"PyCharm.app", "PyCharm CE.app"}, LinuxCommands: []string{"pycharm"}},
	{ID: "intellij", Label: "IntelliJ IDEA", MacAppNames: []string{"IntelliJ IDEA.app", "IntelliJ IDEA CE.app"}, LinuxCommands: []string{"idea", "intellij-idea-community", "intellij-idea-ultimate"}},
	{ID: "webstorm", Label: "WebStorm", MacAppNames: []string{"WebStorm.app"}, LinuxCommands: []string{"webstorm"}},
	{ID: "goland", Label: "GoLand", MacAppNames: []string{"GoLand.app"}, LinuxCommands: []string{"goland"}},
	{ID: "clion", Label: "CLion", MacAppNames: []string{"CLion.app"}, LinuxCommands: []string{"clion"}},
	{ID: "phpstorm", Label: "PhpStorm", MacAppNames: []string{"PhpStorm.app"}, LinuxCommands: []string{"phpstorm"}},
	{ID: "rubymine", Label: "RubyMine", MacAppNames: []string{"RubyMine.app"}, LinuxCommands: []string{"rubymine"}},
	{ID: "fleet", Label: "Fleet", MacAppNames: []string{"Fleet.app"}, LinuxCommands: []string{"fleet"}},
}

func ResolveEditor(path string, preferredCommand string) (ExecSpec, error) {
	editor := strings.TrimSpace(os.Getenv("RUNTREE_EDITOR"))
	if editor != "" {
		return resolveEditorTemplate(editor, path, "RUNTREE_EDITOR")
	}
	if strings.TrimSpace(preferredCommand) != "" {
		return resolveEditorTemplate(preferredCommand, path, "editor preference")
	}

	if detected := AutoDetectEditorCommand(); detected != "" {
		return resolveEditorTemplate(detected, path, "auto-detected editor")
	}

	return ExecSpec{}, errors.New("no editor found: configure `runtree editor`, set RUNTREE_EDITOR, or install a supported editor")
}

func ResolveBrowser(url string) (ExecSpec, error) {
	switch runtime.GOOS {
	case "darwin":
		return ExecSpec{Name: "open", Args: []string{url}}, nil
	case "linux":
		return ExecSpec{Name: "xdg-open", Args: []string{url}}, nil
	default:
		return ExecSpec{}, fmt.Errorf("unsupported platform %s", runtime.GOOS)
	}
}

func Run(spec ExecSpec) error {
	if strings.TrimSpace(spec.Name) == "" {
		return errors.New("command name is required")
	}
	cmd := exec.Command(spec.Name, spec.Args...)
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output

	if err := cmd.Start(); err != nil {
		return err
	}

	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()

	select {
	case err := <-done:
		if err != nil {
			message := strings.TrimSpace(output.String())
			if message != "" {
				return fmt.Errorf("%w: %s", err, message)
			}
			return err
		}
		return nil
	case <-time.After(500 * time.Millisecond):
		return nil
	}
}

func shellQuote(value string) string {
	if value == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(value, "'", `'"'"'`) + "'"
}

func AutoDetectEditorCommand() string {
	for _, preset := range editorPresets {
		command, err := ResolveEditorPresetCommand(preset.ID)
		if err == nil {
			return command
		}
	}
	return ""
}

func SupportedEditorPresets() []EditorPreset {
	presets := make([]EditorPreset, len(editorPresets))
	copy(presets, editorPresets)
	return presets
}

func ResolveEditorPresetCommand(presetID string) (string, error) {
	preset, ok := editorPresetByID(presetID)
	if !ok {
		return "", fmt.Errorf("unknown editor preset %q", presetID)
	}

	switch runtime.GOOS {
	case "darwin":
		for _, appName := range preset.MacAppNames {
			if path, ok := findMacApp(appName); ok {
				return fmt.Sprintf("open -a %s {path}", shellQuote(path)), nil
			}
		}
		return "", fmt.Errorf("%s not found in /Applications or ~/Applications", preset.Label)
	case "linux":
		for _, command := range preset.LinuxCommands {
			if _, err := exec.LookPath(command); err == nil {
				return command + " {path}", nil
			}
		}
		return "", fmt.Errorf("%s not found in PATH", preset.Label)
	default:
		return "", fmt.Errorf("unsupported platform %s", runtime.GOOS)
	}
}

func IsEditorPresetAvailable(presetID string) bool {
	_, err := ResolveEditorPresetCommand(presetID)
	return err == nil
}

func resolveEditorTemplate(template, path, source string) (ExecSpec, error) {
	if !strings.Contains(template, "{path}") {
		return ExecSpec{}, fmt.Errorf("%s must contain {path}", source)
	}
	return ExecSpec{
		Name:     "/bin/sh",
		Args:     []string{"-lc", strings.ReplaceAll(template, "{path}", shellQuote(path))},
		UseShell: true,
	}, nil
}

func findMacApp(name string) (string, bool) {
	home, _ := os.UserHomeDir()
	candidates := []string{
		filepath.Join("/Applications", name),
		filepath.Join(home, "Applications", name),
	}
	for _, candidate := range candidates {
		if _, err := os.Stat(candidate); err == nil {
			return candidate, true
		}
	}
	return "", false
}

func editorPresetByID(presetID string) (EditorPreset, bool) {
	for _, preset := range editorPresets {
		if preset.ID == strings.ToLower(strings.TrimSpace(presetID)) {
			return preset, true
		}
	}
	return EditorPreset{}, false
}
