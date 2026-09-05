package cli_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/scarb/skope/internal/agent"
	"github.com/scarb/skope/internal/cli"
	"github.com/scarb/skope/internal/config"
	"github.com/scarb/skope/internal/launch"
	"github.com/scarb/skope/internal/session"
	"github.com/scarb/skope/internal/skill"
)

func TestExecuteHelpListsRootCommand(t *testing.T) {
	var stdout, stderr bytes.Buffer

	code := cli.Execute([]string{"--help"}, &stdout, &stderr, "test")

	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr=%q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "skope") {
		t.Errorf("help output does not mention skope:\n%s", stdout.String())
	}
	if !strings.Contains(stdout.String(), "skope help [command]") {
		t.Errorf("help output should direct users to the help command:\n%s", stdout.String())
	}
	if strings.Contains(stdout.String(), "[command] --help") {
		t.Errorf("help output should not advertise agent --help as skope help:\n%s", stdout.String())
	}
}

func TestApplicationHelpListsCommandsWithoutRunningDependencies(t *testing.T) {
	var stdout, stderr bytes.Buffer
	app := cli.Application{
		LoadSkillSets: func() (string, config.SkillSets, error) {
			t.Fatal("loader called while rendering help")
			return "", config.SkillSets{}, nil
		},
		RunLaunch: func(context.Context, launch.Request, launch.Reporter) error {
			t.Fatal("launch runner called while rendering help")
			return nil
		},
	}

	code := app.Execute([]string{"--help"}, &stdout, &stderr, "test")

	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr=%q", code, stderr.String())
	}
	for _, command := range []string{"claude", "list", "version"} {
		if !strings.Contains(stdout.String(), command) {
			t.Errorf("help should list the %s command:\n%s", command, stdout.String())
		}
	}
}

func TestClaudeCommandHelpShowsSkopeLaunchUsage(t *testing.T) {
	var stdout, stderr bytes.Buffer

	code := (cli.Application{}).Execute([]string{"help", "claude"}, &stdout, &stderr, "test")

	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr=%q", code, stderr.String())
	}
	want := "Launch claude with a skill set\n\nUsage:\n  skope claude [skope options] [agent args...]\n"
	if stdout.String() != want {
		t.Errorf("help output = %q, want %q", stdout.String(), want)
	}
}

func TestClaudeCommandPassesParsedRequestAndAgentArguments(t *testing.T) {
	want := launch.Request{
		Agent:      skill.AgentClaude,
		SetValue:   " dev, ops,dev ",
		SetPresent: true,
		DryRun:     true,
		AgentArgs:  []string{"--help", "--model", "sonnet"},
	}
	var got launch.Request
	app := cli.Application{RunLaunch: func(_ context.Context, req launch.Request, report launch.Reporter) error {
		got = req
		if report == nil {
			t.Error("runner received a nil reporter")
		}
		return nil
	}}
	var stdout, stderr bytes.Buffer

	code := app.Execute(
		[]string{"claude", "-s", " dev, ops,dev ", "--dry-run", "--", "--help", "--model", "sonnet"},
		&stdout,
		&stderr,
		"test",
	)

	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr=%q", code, stderr.String())
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("request = %#v, want %#v", got, want)
	}
}

func TestClaudeCommandTreatsHelpAsAnAgentArgument(t *testing.T) {
	var got launch.Request
	app := cli.Application{RunLaunch: func(_ context.Context, req launch.Request, _ launch.Reporter) error {
		got = req
		return nil
	}}
	var stdout, stderr bytes.Buffer

	code := app.Execute([]string{"claude", "--help"}, &stdout, &stderr, "test")

	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr=%q", code, stderr.String())
	}
	if !reflect.DeepEqual(got.AgentArgs, []string{"--help"}) {
		t.Errorf("agent args = %q, want [--help]", got.AgentArgs)
	}
}

func TestClaudeCommandReportsParseAndRunnerErrors(t *testing.T) {
	t.Run("parse", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		app := cli.Application{RunLaunch: func(context.Context, launch.Request, launch.Reporter) error {
			t.Fatal("runner called after argument parsing failed")
			return nil
		}}

		code := app.Execute([]string{"claude", "-s", "dev", "--set", "ops"}, &stdout, &stderr, "test")

		if code != 1 {
			t.Fatalf("exit code = %d, want 1", code)
		}
		for _, fragment := range []string{"parse claude launch arguments", "set is repeated"} {
			if !strings.Contains(stderr.String(), fragment) {
				t.Errorf("stderr %q does not contain %q", stderr.String(), fragment)
			}
		}
	})

	t.Run("runner", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		app := cli.Application{RunLaunch: func(context.Context, launch.Request, launch.Reporter) error {
			return errors.New("runner failed")
		}}

		code := app.Execute([]string{"claude", "-s", "dev"}, &stdout, &stderr, "test")

		if code != 1 {
			t.Fatalf("exit code = %d, want 1", code)
		}
		for _, fragment := range []string{"launch claude", "runner failed"} {
			if !strings.Contains(stderr.String(), fragment) {
				t.Errorf("stderr %q does not contain %q", stderr.String(), fragment)
			}
		}
	})

	t.Run("required skill set", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		app := cli.Application{RunLaunch: func(context.Context, launch.Request, launch.Reporter) error {
			return launch.ErrSetRequired
		}}

		code := app.Execute([]string{"claude"}, &stdout, &stderr, "test")

		if code != 1 {
			t.Fatalf("exit code = %d, want 1", code)
		}
		for _, fragment := range []string{"-s <name>", "-s none"} {
			if !strings.Contains(stderr.String(), fragment) {
				t.Errorf("stderr %q does not contain %q", stderr.String(), fragment)
			}
		}
	})

	t.Run("missing runner", func(t *testing.T) {
		var stdout, stderr bytes.Buffer

		code := (cli.Application{}).Execute([]string{"claude", "-s", "dev"}, &stdout, &stderr, "test")

		if code != 1 {
			t.Fatalf("exit code = %d, want 1", code)
		}
		if !strings.Contains(stderr.String(), "launch runner is not configured") {
			t.Errorf("stderr should explain the internal configuration error, got %q", stderr.String())
		}
	})
}

func TestClaudeCommandReportsActiveSkillSetAndAllCollisionKinds(t *testing.T) {
	result := launch.Result{
		Env: []string{"INHERITED_SECRET=do-not-print"},
		Resolved: skill.Resolved{Agent: skill.AgentClaude, Entries: []skill.Resolution{
			{ID: "native-one", State: skill.StateNative},
			{ID: "missing-one", State: skill.StateMissing},
			{ID: "native-two", State: skill.StateNative},
		}},
		Inventory: agent.Inventory{Collisions: []skill.Collision{
			{Kind: skill.CollisionDifferentContent, IDs: []string{"native-one"}, Paths: []string{"/skills/first/SKILL.md", "/skills/second/SKILL.md"}},
			{Kind: skill.CollisionFrontmatterName, IDs: []string{"native-two"}, Name: "declared-name", Paths: []string{"/skills/native-two/SKILL.md"}},
			{Kind: skill.CollisionEffectiveName, IDs: []string{"native-one", "native-two"}, Agent: skill.AgentClaude, Name: "shared-name", Paths: []string{"/skills/first/SKILL.md", "/skills/native-two/SKILL.md"}},
		}},
	}
	app := cli.Application{RunLaunch: reportingRunner(t, result)}
	var stdout, stderr bytes.Buffer

	code := app.Execute([]string{"claude", "-s", " dev, ops,dev "}, &stdout, &stderr, "test")

	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr=%q", code, stderr.String())
	}
	output := stdout.String()
	for _, fragment := range []string{
		"Skillset [dev+ops] for claude",
		"2 native",
		"1 missing",
		"missing-one",
		"different-content",
		"native-one",
		"/skills/first/SKILL.md",
		"/skills/second/SKILL.md",
		"frontmatter-name",
		"native-two",
		"declared-name",
		"/skills/native-two/SKILL.md",
		"effective-name",
		"shared-name",
		"Launching claude",
	} {
		if !strings.Contains(output, fragment) {
			t.Errorf("output does not contain %q:\n%s", fragment, output)
		}
	}
	if strings.Contains(output, "do-not-print") {
		t.Errorf("output leaked an inherited environment value:\n%s", output)
	}
	wantCollisionLines := []string{
		"  collision different-content: IDs=native-one paths=/skills/first/SKILL.md,/skills/second/SKILL.md\n",
		"  collision frontmatter-name: IDs=native-two name=declared-name paths=/skills/native-two/SKILL.md\n",
		"  collision effective-name: IDs=native-one,native-two agent=claude name=shared-name paths=/skills/first/SKILL.md,/skills/native-two/SKILL.md\n",
	}
	previous := -1
	for _, line := range wantCollisionLines {
		index := strings.Index(output, line)
		if index < 0 {
			t.Errorf("output does not contain complete collision line %q:\n%s", line, output)
			continue
		}
		if index <= previous {
			t.Errorf("collision line %q is out of order:\n%s", line, output)
		}
		previous = index
	}
}

func TestClaudeCommandReportsNoneWithoutInventorySummary(t *testing.T) {
	result := launch.Result{
		Env:         []string{"SECRET=do-not-print"},
		NoIsolation: true,
	}
	app := cli.Application{RunLaunch: reportingRunner(t, result)}
	var stdout, stderr bytes.Buffer

	code := app.Execute([]string{"claude", "-s", "none"}, &stdout, &stderr, "test")

	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr=%q", code, stderr.String())
	}
	output := stdout.String()
	for _, fragment := range []string{"Skillset [none] for claude", "no isolation", "Launching claude"} {
		if !strings.Contains(output, fragment) {
			t.Errorf("output does not contain %q:\n%s", fragment, output)
		}
	}
	for _, unwanted := range []string{"native", "missing", "do-not-print"} {
		if strings.Contains(output, unwanted) {
			t.Errorf("output unexpectedly contains %q:\n%s", unwanted, output)
		}
	}
}

func TestClaudeCommandDryRunReportsArgvAndSessionFilesWithoutEnvironment(t *testing.T) {
	root := filepath.Join(t.TempDir(), "skope", "sessions", "preview")
	settingsPath := filepath.Join(root, "claude", "settings.json")
	settings := []byte("{\n  \"skillOverrides\": {\n    \"review\": \"on\"\n  }\n}\n")
	result := launch.Result{
		Executable: `C:\Program Files\Claude\claude.exe`,
		Args:       []string{"--model", "claude sonnet", "--settings", settingsPath},
		Env:        []string{"INHERITED_SECRET=do-not-print", "CLAUDE_CONFIG_DIR=also-secret"},
		Resolved: skill.Resolved{Agent: skill.AgentClaude, Entries: []skill.Resolution{
			{ID: "review", State: skill.StateNative},
		}},
		Plan: agent.LaunchPlan{Files: []agent.PlannedFile{{
			Path: settingsPath,
			Data: settings,
		}}},
		Session: &session.Session{Root: root, Agent: skill.AgentClaude},
	}
	app := cli.Application{RunLaunch: reportingRunner(t, result)}
	var stdout, stderr bytes.Buffer

	code := app.Execute([]string{"claude", "-s", "dev", "--dry-run"}, &stdout, &stderr, "test")

	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr=%q", code, stderr.String())
	}
	output := stdout.String()
	for _, fragment := range []string{
		"Executable: C:\\Program Files\\Claude\\claude.exe",
		`[0] "C:\\Program Files\\Claude\\claude.exe"`,
		`[1] "--model"`,
		`[2] "claude sonnet"`,
		`[3] "--settings"`,
		"claude/settings.json",
		`"skillOverrides"`,
		`"review": "on"`,
	} {
		if !strings.Contains(output, fragment) {
			t.Errorf("output does not contain %q:\n%s", fragment, output)
		}
	}
	for _, unwanted := range []string{"do-not-print", "also-secret", "Launching claude"} {
		if strings.Contains(output, unwanted) {
			t.Errorf("output unexpectedly contains %q:\n%s", unwanted, output)
		}
	}
}

func TestClaudeCommandDryRunRejectsFilesOutsideSessionRoot(t *testing.T) {
	root := filepath.Join(t.TempDir(), "sessions", "x")
	escapePath := strings.Join([]string{root, "..", "escape", "settings.json"}, string(filepath.Separator))
	tests := []struct {
		name string
		path string
	}{
		{name: "session root", path: root},
		{name: "session parent", path: filepath.Dir(root)},
		{name: "sibling", path: filepath.Join(filepath.Dir(root), "sibling", "settings.json")},
		{name: "parent escape", path: escapePath},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result := launch.Result{
				Executable: "claude",
				Resolved:   skill.Resolved{Agent: skill.AgentClaude},
				Plan: agent.LaunchPlan{Files: []agent.PlannedFile{{
					Path: test.path,
					Data: []byte("{}\n"),
				}}},
				Session: &session.Session{Root: root, Agent: skill.AgentClaude},
			}
			app := cli.Application{RunLaunch: reportingRunner(t, result)}
			var stdout, stderr bytes.Buffer

			code := app.Execute([]string{"claude", "-s", "dev", "--dry-run"}, &stdout, &stderr, "test")

			if code != 1 {
				t.Fatalf("exit code = %d, want 1", code)
			}
			if !strings.Contains(stderr.String(), "outside session root") {
				t.Errorf("stderr should identify the invalid file path, got %q", stderr.String())
			}
			relative, err := filepath.Rel(root, test.path)
			if err != nil {
				t.Fatal(err)
			}
			invalidLine := "  " + filepath.ToSlash(relative) + "\n"
			if strings.Contains(stdout.String(), invalidLine) {
				t.Errorf("output printed invalid session file %q:\n%s", invalidLine, stdout.String())
			}
		})
	}
}

func TestClaudeCommandReturnsReporterWriterErrors(t *testing.T) {
	app := cli.Application{RunLaunch: reportingRunner(t, launch.Result{
		Resolved: skill.Resolved{Agent: skill.AgentClaude},
	})}
	var stderr bytes.Buffer

	code := app.Execute([]string{"claude", "-s", "dev"}, errorWriter{}, &stderr, "test")

	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "write failed") {
		t.Errorf("stderr should contain the writer error, got %q", stderr.String())
	}
}

func reportingRunner(t *testing.T, result launch.Result) func(context.Context, launch.Request, launch.Reporter) error {
	t.Helper()
	return func(_ context.Context, _ launch.Request, report launch.Reporter) error {
		if report == nil {
			t.Fatal("runner received a nil reporter")
		}
		return report(result)
	}
}

type errorWriter struct{}

func (errorWriter) Write([]byte) (int, error) {
	return 0, errors.New("write failed")
}

var _ io.Writer = errorWriter{}

func TestZeroApplicationAllowsHelpAndVersion(t *testing.T) {
	for _, args := range [][]string{{"--help"}, {"version"}} {
		var stdout, stderr bytes.Buffer

		code := (cli.Application{}).Execute(args, &stdout, &stderr, "test")

		if code != 0 {
			t.Errorf("args %q: exit code = %d, want 0; stderr=%q", args, code, stderr.String())
		}
	}
}

func TestExecuteUnknownCommandReturnsOne(t *testing.T) {
	var stdout, stderr bytes.Buffer

	code := cli.Execute([]string{"no-such-command"}, &stdout, &stderr, "test")

	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "no-such-command") {
		t.Errorf("stderr should name the unknown command, got %q", stderr.String())
	}
}

func TestApplicationPropagatesConflictError(t *testing.T) {
	app := cli.Application{RunLaunch: func(context.Context, launch.Request, launch.Reporter) error {
		return &cli.ConflictError{Flag: "--settings", Source: "command-line"}
	}}
	var stdout, stderr bytes.Buffer
	if code := app.Execute([]string{"claude", "-s", "dev"}, &stdout, &stderr, "test"); code != 1 || !strings.Contains(stderr.String(), "--settings") || !strings.Contains(stderr.String(), "command-line") {
		t.Fatalf("code=%d stderr=%s", code, &stderr)
	}
}

func TestHelpUnknownTopicUsesSafeErrorExit(t *testing.T) {
	for _, topic := range []string{"unknown\u202e", "unknown\x1b[31m"} {
		var stdout, stderr bytes.Buffer
		code := (cli.Application{}).Execute([]string{"help", topic}, &stdout, &stderr, "")
		if code != 1 || stdout.Len() != 0 || strings.Count(stderr.String(), "Error:") != 1 {
			t.Errorf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
		}
		assertNoTerminalControls(t, stdout.String()+stderr.String())
	}
}

type controlledErrorWriter struct{}

func (controlledErrorWriter) Write([]byte) (int, error) {
	return 0, errors.New("writer\x1b[31m\n\u202e")
}

func TestHelpWriterErrorsUseOneSafeErrorExit(t *testing.T) {
	for _, args := range [][]string{{"--help"}, {"help", "claude"}, {"list", "--help"}, {"completion", "--help"}, {"completion", "bash"}, {"completion", "zsh"}, {"completion", "fish"}, {"completion", "powershell"}} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			var stderr bytes.Buffer
			code := (cli.Application{}).Execute(args, controlledErrorWriter{}, &stderr, "")
			if code != 1 || strings.Count(stderr.String(), "writer") != 1 || strings.Count(stderr.String(), "Error:") != 1 {
				t.Errorf("code=%d stderr=%q", code, stderr.String())
			}
			assertNoTerminalControls(t, stderr.String())
		})
	}
}

type helpUsageErrorWriter struct{ wroteDescription bool }

func (w *helpUsageErrorWriter) Write(p []byte) (int, error) {
	if !w.wroteDescription {
		w.wroteDescription = true
		return len(p), nil
	}
	return controlledErrorWriter{}.Write(p)
}

func TestHelpUsageTemplateErrorIsCapturedOnce(t *testing.T) {
	for _, args := range [][]string{{"--help"}, {"help", "claude"}} {
		var stderr bytes.Buffer
		code := (cli.Application{}).Execute(args, &helpUsageErrorWriter{}, &stderr, "")
		if code != 1 || strings.Count(stderr.String(), "writer") != 1 || strings.Count(stderr.String(), "Error:") != 1 {
			t.Fatalf("code=%d stderr=%q", code, stderr.String())
		}
		assertNoTerminalControls(t, stderr.String())
	}
}

func TestHelpUnknownTopicWriterFailureReturnsWithoutExitingProcess(t *testing.T) {
	if os.Getenv("SKOPE_TEST_HELP_CHILD") == "1" {
		var stderr bytes.Buffer
		code := (cli.Application{}).Execute([]string{"help", "unknown\u202e"}, controlledErrorWriter{}, &stderr, "")
		if code != 1 {
			t.Fatalf("code=%d", code)
		}
		assertNoTerminalControls(t, stderr.String())
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestHelpUnknownTopicWriterFailureReturnsWithoutExitingProcess$")
	cmd.Env = append(os.Environ(), "SKOPE_TEST_HELP_CHILD=1")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("Application.Execute exited helper process: %v\n%s", err, out)
	}
}
