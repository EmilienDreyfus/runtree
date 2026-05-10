package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"text/tabwriter"
	"time"

	"github.com/AlecAivazis/survey/v2"
	"github.com/spf13/cobra"

	"github.com/EmilienDreyfus/runtree/internal/app"
	"github.com/EmilienDreyfus/runtree/internal/authflow"
	"github.com/EmilienDreyfus/runtree/internal/authstore"
	"github.com/EmilienDreyfus/runtree/internal/buildinfo"
	"github.com/EmilienDreyfus/runtree/internal/cloudapi"
	"github.com/EmilienDreyfus/runtree/internal/config"
	"github.com/EmilienDreyfus/runtree/internal/expose"
	"github.com/EmilienDreyfus/runtree/internal/openers"
	"github.com/EmilienDreyfus/runtree/internal/settings"
	"github.com/EmilienDreyfus/runtree/internal/state"
	"github.com/EmilienDreyfus/runtree/internal/tunnel"
)

func NewRootCommand() *cobra.Command {
	service := app.NewService("")

	rootCmd := &cobra.Command{
		Use:          "runtree",
		Short:        "Run multiple realities of your codebase.",
		SilenceUsage: true,
		Version:      buildinfo.Version,
	}
	rootCmd.SetVersionTemplate("{{.Name}} {{.Version}}\n")

	rootCmd.AddCommand(newInitCommand(service))
	rootCmd.AddCommand(newLoginCommand())
	rootCmd.AddCommand(newLogoutCommand())
	rootCmd.AddCommand(newEditorCommand())
	rootCmd.AddCommand(newListCommand(service))
	rootCmd.AddCommand(newPruneCommand(service))
	rootCmd.AddCommand(newUpCommand(service))
	rootCmd.AddCommand(newDownCommand(service))
	rootCmd.AddCommand(newRestartCommand(service))
	rootCmd.AddCommand(newLogsCommand(service))
	rootCmd.AddCommand(newWebCommand(service))
	rootCmd.AddCommand(newCodeCommand(service))
	rootCmd.AddCommand(newExposeCommand(service))
	rootCmd.AddCommand(newVersionCommand())

	return rootCmd
}

func newLoginCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "login",
		Short: "Sign in to runtree cloud in your browser",
		RunE: func(cmd *cobra.Command, args []string) error {
			client := cloudapi.NewClient(resolveCloudBaseURL(""), "")
			service := authflow.Service{
				BaseURL: resolveCloudBaseURL(""),
				Client:  client,
				OpenBrowser: func(target string) error {
					spec, err := openers.ResolveBrowser(target)
					if err != nil {
						return err
					}
					return openers.Run(spec)
				},
			}
			_, err := service.Login(cmd.Context(), cmd.OutOrStdout())
			return err
		},
	}
}

func newLogoutCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "logout",
		Short: "Clear the local runtree cloud session",
		RunE: func(cmd *cobra.Command, args []string) error {
			service := authflow.Service{}
			if err := service.Logout(); err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), "logged out")
			return nil
		},
	}
}

func newVersionCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Show runtree version information",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Fprintln(cmd.OutOrStdout(), buildinfo.Details())
		},
	}
}

func newInitCommand(service app.Service) *cobra.Command {
	var input app.InitInput
	var editorPreset string
	var editorCommand string

	cmd := &cobra.Command{
		Use:   "init",
		Short: "Initialize runtree for the current Git repository",
		RunE: func(cmd *cobra.Command, args []string) error {
			var err error
			input, err = completeInitInput(input, isInteractive())
			if err != nil {
				return err
			}
			ctx, err := service.InitProject(mustGetwd(), input)
			if err != nil {
				return err
			}
			savedEditor, err := ensureEditorPreference(editorPreset, editorCommand, isInteractive())
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "initialized %s at %s\n", ctx.Project.Name, ctx.ConfigPath)
			if savedEditor != "" {
				fmt.Fprintf(cmd.OutOrStdout(), "saved editor preference: %s\n", savedEditor)
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&input.Name, "name", "", "project name")
	cmd.Flags().StringVar(&input.RunCommand, "run-command", "", "runtime command with {port}")
	cmd.Flags().IntVar(&input.PortStart, "port-start", config.DefaultPortStart, "port range start")
	cmd.Flags().IntVar(&input.PortEnd, "port-end", config.DefaultPortEnd, "port range end")
	cmd.Flags().StringVar(&input.WebURLTemplate, "web-url-template", config.DefaultWebURLFormat, "web URL template")
	cmd.Flags().StringVar(&editorPreset, "editor", "", "preferred editor preset (cursor, codex, vscode, pycharm, intellij, webstorm, goland, clion, phpstorm, rubymine, fleet, zed, windsurf)")
	cmd.Flags().StringVar(&editorCommand, "editor-command", "", "preferred editor command template containing {path}")

	return cmd
}

func newEditorCommand() *cobra.Command {
	var preset string
	var command string
	var show bool
	var reset bool
	var list bool

	cmd := &cobra.Command{
		Use:   "editor",
		Short: "Show or configure the preferred editor used by `runtree code`",
		RunE: func(cmd *cobra.Command, args []string) error {
			current, err := settings.Load("")
			if err != nil {
				return err
			}
			if show {
				if strings.TrimSpace(current.EditorCommand) == "" {
					fmt.Fprintln(cmd.OutOrStdout(), "no editor preference saved")
					return nil
				}
				fmt.Fprintln(cmd.OutOrStdout(), current.EditorCommand)
				return nil
			}
			if list {
				for _, preset := range openers.SupportedEditorPresets() {
					status := "unavailable"
					if openers.IsEditorPresetAvailable(preset.ID) {
						status = "available"
					}
					fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\t%s\n", preset.ID, preset.Label, status)
				}
				return nil
			}
			if reset {
				if err := settings.Save("", settings.Settings{}); err != nil {
					return err
				}
				fmt.Fprintln(cmd.OutOrStdout(), "cleared editor preference")
				return nil
			}

			if strings.TrimSpace(preset) == "" && strings.TrimSpace(command) == "" {
				if !isInteractive() {
					if strings.TrimSpace(current.EditorCommand) == "" {
						fmt.Fprintln(cmd.OutOrStdout(), "no editor preference saved")
						return nil
					}
					fmt.Fprintln(cmd.OutOrStdout(), current.EditorCommand)
					return nil
				}
				selection, custom, err := promptEditorPreference()
				if err != nil {
					return err
				}
				switch selection {
				case "":
					fmt.Fprintln(cmd.OutOrStdout(), "no editor preference saved")
					return nil
				case "custom":
					command = custom
				default:
					preset = selection
				}
			}

			saved, err := saveEditorPreference(preset, command)
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "saved editor preference: %s\n", saved)
			return nil
		},
	}

	cmd.Flags().StringVar(&preset, "use", "", "editor preset to save")
	cmd.Flags().StringVar(&command, "command", "", "editor command template containing {path}")
	cmd.Flags().BoolVar(&show, "show", false, "show the current saved editor preference")
	cmd.Flags().BoolVar(&reset, "reset", false, "clear the saved editor preference")
	cmd.Flags().BoolVar(&list, "list", false, "list supported editor presets and their availability")
	return cmd
}

func newListCommand(service app.Service) *cobra.Command {
	var includeIgnored bool

	cmd := &cobra.Command{
		Use:   "ls",
		Short: "List runtree instances and import new worktrees",
		RunE: func(cmd *cobra.Command, args []string) error {
			interactive := isInteractive()
			var prompter app.ImportPrompter
			if interactive {
				prompter = surveyImportPrompter{out: cmd.OutOrStdout()}
			}

			result, err := service.Inventory(mustGetwd(), includeIgnored, interactive, prompter)
			if err != nil {
				return err
			}

			out := cmd.OutOrStdout()
			if len(result.Imported) > 0 {
				fmt.Fprintf(out, "imported %d worktree%s\n\n", len(result.Imported), pluralS(len(result.Imported)))
			}

			if len(result.Instances) == 0 {
				fmt.Fprintln(out, "no instances")
			} else if err := printInstances(out, result.Instances); err != nil {
				return err
			}

			remaining := remainingCandidates(result)
			if len(remaining) > 0 && !interactive {
				if len(result.Instances) > 0 {
					fmt.Fprintln(out)
				}
				if err := printUnmanagedWorktrees(out, remaining); err != nil {
					return err
				}
				fmt.Fprintln(out, "\nrun `runtree ls` in an interactive shell to import these worktrees")
			}
			return nil
		},
	}

	cmd.Flags().BoolVar(&includeIgnored, "all", false, "include ignored internal instances")
	return cmd
}

func newPruneCommand(service app.Service) *cobra.Command {
	return &cobra.Command{
		Use:   "prune",
		Short: "Remove ignored internal instances from local runtree state",
		RunE: func(cmd *cobra.Command, args []string) error {
			result, err := service.PruneInstances(mustGetwd())
			if err != nil {
				return err
			}

			out := cmd.OutOrStdout()
			if len(result.Pruned) == 0 && len(result.SkippedRunning) == 0 {
				fmt.Fprintln(out, "nothing to prune")
				return nil
			}
			for _, instance := range result.Pruned {
				fmt.Fprintf(out, "pruned %s (%s)\n", instance.Name, instance.WorktreePath)
			}
			for _, instance := range result.SkippedRunning {
				fmt.Fprintf(out, "skipped running %s (%s)\n", instance.Name, instance.WorktreePath)
			}
			return nil
		},
	}
}

func newUpCommand(service app.Service) *cobra.Command {
	return &cobra.Command{
		Use:     "up <instance>",
		Aliases: []string{"start"},
		Short:   "Start a runtree instance",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			instance, err := service.StartInstance(mustGetwd(), args[0])
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "running %s on http://127.0.0.1:%d (pid %d)\n", instance.Name, instance.Port, instance.PID)
			return nil
		},
	}
}

func newDownCommand(service app.Service) *cobra.Command {
	return &cobra.Command{
		Use:     "down <instance>",
		Aliases: []string{"stop"},
		Short:   "Stop a runtree instance",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			instance, err := service.StopInstance(mustGetwd(), args[0])
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "stopped %s\n", instance.Name)
			return nil
		},
	}
}

func newRestartCommand(service app.Service) *cobra.Command {
	return &cobra.Command{
		Use:   "restart <instance>",
		Short: "Restart a runtree instance",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			instance, err := service.RestartInstance(mustGetwd(), args[0])
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "restarted %s on http://127.0.0.1:%d (pid %d)\n", instance.Name, instance.Port, instance.PID)
			return nil
		},
	}
}

func newLogsCommand(service app.Service) *cobra.Command {
	var follow bool

	cmd := &cobra.Command{
		Use:   "logs <instance>",
		Short: "Read or stream logs for a runtree instance",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			_, instance, err := service.InstanceDetails(mustGetwd(), args[0])
			if err != nil {
				return err
			}

			followLogs := follow || isInteractive()
			return streamLog(cmd.OutOrStdout(), instance.LogPath, followLogs)
		},
	}

	cmd.Flags().BoolVarP(&follow, "follow", "f", false, "follow log output")
	return cmd
}

func newWebCommand(service app.Service) *cobra.Command {
	return &cobra.Command{
		Use:   "web <instance>",
		Short: "Open an instance in the browser",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, instance, err := service.InstanceDetails(mustGetwd(), args[0])
			if err != nil {
				return err
			}
			url := ctx.Config.RenderWebURL(instance.Port)
			spec, err := openers.ResolveBrowser(url)
			if err != nil {
				return err
			}
			if err := openers.Run(spec); err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), url)
			return nil
		},
	}
}

func newCodeCommand(service app.Service) *cobra.Command {
	return &cobra.Command{
		Use:   "code <instance>",
		Short: "Open an instance worktree in the editor",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			_, instance, err := service.InstanceDetails(mustGetwd(), args[0])
			if err != nil {
				return err
			}
			if instance.Status == state.StatusMissing {
				return fmt.Errorf("instance %s is missing its worktree", instance.Name)
			}
			editorSettings, err := settings.Load("")
			if err != nil {
				return err
			}
			spec, err := openers.ResolveEditor(instance.WorktreePath, editorSettings.EditorCommand)
			if err != nil {
				return err
			}
			if err := openers.Run(spec); err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), instance.WorktreePath)
			return nil
		},
	}
}

func newExposeCommand(service app.Service) *cobra.Command {
	var tunnelLogs bool
	cmd := &cobra.Command{
		Use:   "expose <instance>",
		Short: "Expose an instance through runtree cloud",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			auth, err := authstore.Load("")
			if err != nil {
				return err
			}
			if strings.TrimSpace(auth.AccessToken) == "" {
				return errors.New("not logged in: run `runtree login` first")
			}
			if _, err := tunnel.ResolveBinaryPath(""); err != nil {
				return err
			}

			baseURL := resolveCloudBaseURL(auth.BaseURL)
			client := cloudapi.NewClient(baseURL, auth.AccessToken)
			runner := tunnel.Runner{}
			if tunnelLogs {
				runner.Stdout = cmd.OutOrStdout()
				runner.Stderr = cmd.ErrOrStderr()
			}
			controller := expose.Service{
				App:    service,
				Cloud:  client,
				Runner: runner,
				Log:    cmd.ErrOrStderr(),
				OnReady: func(state expose.RunState) {
					fmt.Fprintf(cmd.OutOrStdout(), "public URL: %s\n", state.PublicURL)
				},
			}

			ctx, stop := signalContext(cmd.Context())
			defer stop()

			err = controller.Run(ctx, mustGetwd(), args[0])
			if err == nil || errors.Is(err, context.Canceled) {
				return nil
			}

			var apiErr *cloudapi.APIError
			if errors.As(err, &apiErr) && apiErr.UpgradeURL != "" {
				return fmt.Errorf("%s\nupgrade: %s", apiErr.Message, apiErr.UpgradeURL)
			}
			return err
		},
	}
	cmd.Flags().BoolVar(&tunnelLogs, "tunnel-logs", false, "stream tunnel provider logs")
	return cmd
}

type surveyImportPrompter struct {
	out io.Writer
}

func (p surveyImportPrompter) SelectImports(candidates []app.WorktreeCandidate) ([]app.ImportDecision, error) {
	if len(candidates) == 0 {
		return nil, nil
	}

	if p.out != nil {
		if err := printUnmanagedWorktrees(p.out, candidates); err != nil {
			return nil, err
		}
		fmt.Fprintln(p.out)
	}

	const (
		importAll = "Import all"
		choose    = "Choose worktrees"
		ignore    = "Ignore for now"
	)

	action := importAll
	if err := survey.AskOne(&survey.Select{
		Message: fmt.Sprintf("Found %d unmanaged worktree%s:", len(candidates), pluralS(len(candidates))),
		Options: []string{importAll, choose, ignore},
		Default: importAll,
	}, &action); err != nil {
		return nil, err
	}

	switch action {
	case importAll:
		decisions := make([]app.ImportDecision, 0, len(candidates))
		for _, candidate := range candidates {
			decisions = append(decisions, app.ImportDecision{
				WorktreePath: candidate.WorktreePath,
				Name:         candidate.SuggestedName,
			})
		}
		return decisions, nil
	case choose:
		return promptImportSelection(candidates)
	default:
		return nil, nil
	}
}

func promptImportSelection(candidates []app.WorktreeCandidate) ([]app.ImportDecision, error) {
	labels := make([]string, 0, len(candidates))
	byLabel := make(map[string]app.WorktreeCandidate, len(candidates))
	for _, candidate := range candidates {
		label := worktreeCandidateLabel(candidate)
		labels = append(labels, label)
		byLabel[label] = candidate
	}

	var selected []string
	if err := survey.AskOne(&survey.MultiSelect{
		Message: "Worktrees to import:",
		Options: labels,
	}, &selected); err != nil {
		return nil, err
	}

	usedNames := map[string]bool{}
	for _, reserved := range candidates[0].ReservedNames {
		usedNames[reserved] = true
	}

	decisions := make([]app.ImportDecision, 0, len(selected))
	for _, label := range selected {
		candidate := byLabel[label]
		name, err := promptInstanceName(candidate, usedNames)
		if err != nil {
			return nil, err
		}
		usedNames[name] = true
		decisions = append(decisions, app.ImportDecision{
			WorktreePath: candidate.WorktreePath,
			Name:         name,
		})
	}
	return decisions, nil
}

func promptInstanceName(candidate app.WorktreeCandidate, usedNames map[string]bool) (string, error) {
	name := candidate.SuggestedName
	validator := func(value any) error {
		name := strings.TrimSpace(value.(string))
		if name == "" {
			return errors.New("instance name is required")
		}
		if usedNames[name] {
			return fmt.Errorf("instance name %q already exists", name)
		}
		return nil
	}
	if err := survey.AskOne(&survey.Input{
		Message: fmt.Sprintf("Instance name for %s:", filepath.Base(candidate.WorktreePath)),
		Default: candidate.SuggestedName,
	}, &name, survey.WithValidator(validator)); err != nil {
		return "", err
	}
	return strings.TrimSpace(name), nil
}

func printInstances(out io.Writer, instances []state.Instance) error {
	tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "INSTANCE\tBRANCH\tSTATUS\tPORT\tPID\tWORKTREE")
	for _, instance := range instances {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%d\t%d\t%s\n",
			instance.Name,
			emptyDash(instance.Branch),
			instance.Status,
			instance.Port,
			instance.PID,
			instance.WorktreePath,
		)
	}
	return tw.Flush()
}

func printUnmanagedWorktrees(out io.Writer, candidates []app.WorktreeCandidate) error {
	fmt.Fprintln(out, "unmanaged worktrees:")
	tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "WORKTREE\tBRANCH\tSUGGESTED\tPORT")
	for _, candidate := range candidates {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%d\n",
			candidate.WorktreePath,
			emptyDash(candidate.Branch),
			candidate.SuggestedName,
			candidate.Port,
		)
	}
	return tw.Flush()
}

func remainingCandidates(result app.InventoryResult) []app.WorktreeCandidate {
	importedPaths := make(map[string]bool, len(result.Imported))
	for _, instance := range result.Imported {
		importedPaths[instance.WorktreePath] = true
	}

	remaining := make([]app.WorktreeCandidate, 0, len(result.Candidates)-len(importedPaths))
	for _, candidate := range result.Candidates {
		if importedPaths[candidate.WorktreePath] {
			continue
		}
		remaining = append(remaining, candidate)
	}
	return remaining
}

func worktreeCandidateLabel(candidate app.WorktreeCandidate) string {
	return fmt.Sprintf("%s (%s) -> %s :%d",
		filepath.Base(candidate.WorktreePath),
		emptyDash(candidate.Branch),
		candidate.SuggestedName,
		candidate.Port,
	)
}

func pluralS(count int) string {
	if count == 1 {
		return ""
	}
	return "s"
}

func completeInitInput(input app.InitInput, interactive bool) (app.InitInput, error) {
	if strings.TrimSpace(input.Name) == "" {
		if !interactive {
			return input, errors.New("--name is required in non-interactive mode")
		}
		if err := survey.AskOne(&survey.Input{Message: "Project name:"}, &input.Name, survey.WithValidator(requiredValue)); err != nil {
			return input, err
		}
	}
	if strings.TrimSpace(input.RunCommand) == "" {
		if !interactive {
			return input, errors.New("--run-command is required in non-interactive mode")
		}
		if err := survey.AskOne(&survey.Input{Message: "Run command:", Default: "uv run python manage.py runserver 127.0.0.1:{port}"}, &input.RunCommand, survey.WithValidator(requiredValue)); err != nil {
			return input, err
		}
	}

	if input.PortStart == 0 {
		input.PortStart = config.DefaultPortStart
	}
	if input.PortEnd == 0 {
		input.PortEnd = config.DefaultPortEnd
	}
	if input.WebURLTemplate == "" {
		input.WebURLTemplate = config.DefaultWebURLFormat
	}

	if interactive {
		portStart := strconv.Itoa(input.PortStart)
		portEnd := strconv.Itoa(input.PortEnd)
		webTemplate := input.WebURLTemplate

		if err := survey.AskOne(&survey.Input{Message: "Port range start:", Default: portStart}, &portStart, survey.WithValidator(requiredValue)); err != nil {
			return input, err
		}
		if err := survey.AskOne(&survey.Input{Message: "Port range end:", Default: portEnd}, &portEnd, survey.WithValidator(requiredValue)); err != nil {
			return input, err
		}
		if err := survey.AskOne(&survey.Input{Message: "Web URL template:", Default: webTemplate}, &webTemplate, survey.WithValidator(requiredValue)); err != nil {
			return input, err
		}

		start, err := strconv.Atoi(strings.TrimSpace(portStart))
		if err != nil {
			return input, fmt.Errorf("invalid port range start: %w", err)
		}
		end, err := strconv.Atoi(strings.TrimSpace(portEnd))
		if err != nil {
			return input, fmt.Errorf("invalid port range end: %w", err)
		}
		input.PortStart = start
		input.PortEnd = end
		input.WebURLTemplate = strings.TrimSpace(webTemplate)
	}

	return input, nil
}

func ensureEditorPreference(preset, command string, interactive bool) (string, error) {
	if strings.TrimSpace(os.Getenv("RUNTREE_EDITOR")) != "" {
		return "", nil
	}

	current, err := settings.Load("")
	if err != nil {
		return "", err
	}
	if current.EditorCommand != "" && strings.TrimSpace(preset) == "" && strings.TrimSpace(command) == "" {
		return "", nil
	}

	if preset != "" && command != "" {
		return "", errors.New("use either an editor preset or an editor command, not both")
	}

	chosenCommand := strings.TrimSpace(command)

	if chosenCommand == "" && interactive {
		selection, custom, err := promptEditorPreference()
		if err != nil {
			return "", err
		}
		if selection == "" {
			return "", nil
		}
		switch selection {
		case "custom":
			chosenCommand = custom
		default:
			preset = selection
		}
	}

	if strings.TrimSpace(preset) == "" && chosenCommand == "" {
		return "", nil
	}

	return saveEditorPreference(preset, chosenCommand)
}

func saveEditorPreference(preset, command string) (string, error) {
	if preset != "" && command != "" {
		return "", errors.New("use either an editor preset or an editor command, not both")
	}

	chosenCommand := strings.TrimSpace(command)
	if preset != "" {
		resolved, err := editorPresetCommand(preset)
		if err != nil {
			return "", err
		}
		chosenCommand = resolved
	}
	if chosenCommand == "" {
		return "", nil
	}

	next := settings.Settings{EditorCommand: chosenCommand}
	if err := settings.Save("", next); err != nil {
		return "", err
	}
	return chosenCommand, nil
}

func promptEditorPreference() (string, string, error) {
	type choice struct {
		label string
		value string
	}

	choices := []choice{}
	for _, preset := range openers.SupportedEditorPresets() {
		if openers.IsEditorPresetAvailable(preset.ID) {
			choices = append(choices, choice{label: preset.Label, value: preset.ID})
		}
	}
	choices = append(choices,
		choice{label: "Custom command", value: "custom"},
		choice{label: "Skip for now", value: ""},
	)

	labels := make([]string, 0, len(choices))
	for _, choice := range choices {
		labels = append(labels, choice.label)
	}

	selectedLabel := ""
	defaultLabel := labels[0]
	if len(labels) == 0 {
		defaultLabel = "Skip for now"
	}
	if err := survey.AskOne(&survey.Select{
		Message: "Preferred editor for `runtree code`:",
		Options: labels,
		Default: defaultLabel,
	}, &selectedLabel); err != nil {
		return "", "", err
	}

	selectedValue := ""
	for _, choice := range choices {
		if choice.label == selectedLabel {
			selectedValue = choice.value
			break
		}
	}
	if selectedValue != "custom" {
		return selectedValue, "", nil
	}

	custom := ""
	if err := survey.AskOne(&survey.Input{
		Message: "Editor command template:",
		Default: "open -a /Applications/Cursor.app {path}",
	}, &custom, survey.WithValidator(requiredEditorCommand)); err != nil {
		return "", "", err
	}
	return selectedValue, custom, nil
}

func editorPresetCommand(preset string) (string, error) {
	return openers.ResolveEditorPresetCommand(preset)
}

func requiredEditorCommand(value any) error {
	command := strings.TrimSpace(value.(string))
	if command == "" {
		return errors.New("value is required")
	}
	if !strings.Contains(command, "{path}") {
		return errors.New("editor command must contain {path}")
	}
	return nil
}

func streamLog(out io.Writer, path string, follow bool) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()

	offset, err := io.Copy(out, file)
	if err != nil {
		return err
	}
	if !follow {
		return nil
	}

	for {
		time.Sleep(250 * time.Millisecond)
		stat, err := file.Stat()
		if err != nil {
			return err
		}
		if stat.Size() < offset {
			if _, err := file.Seek(0, io.SeekStart); err != nil {
				return err
			}
			offset = 0
		}
		if stat.Size() == offset {
			continue
		}
		if _, err := file.Seek(offset, io.SeekStart); err != nil {
			return err
		}
		n, err := io.Copy(out, file)
		offset += n
		if err != nil {
			return err
		}
	}
}

func isInteractive() bool {
	stat, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return (stat.Mode() & os.ModeCharDevice) != 0
}

func requiredValue(value any) error {
	if strings.TrimSpace(value.(string)) == "" {
		return errors.New("value is required")
	}
	return nil
}

func emptyDash(value string) string {
	if strings.TrimSpace(value) == "" {
		return "-"
	}
	return value
}

func mustGetwd() string {
	wd, err := os.Getwd()
	if err != nil {
		panic(err)
	}
	return wd
}

func resolveCloudBaseURL(saved string) string {
	if override := strings.TrimSpace(os.Getenv("RUNTREE_CLOUD_URL")); override != "" {
		return override
	}
	if strings.TrimSpace(saved) != "" {
		return saved
	}
	return cloudapi.DefaultBaseURL
}

func signalContext(parent context.Context) (context.Context, context.CancelFunc) {
	return signal.NotifyContext(parent, os.Interrupt, syscall.SIGTERM)
}
