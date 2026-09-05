package cli_test

import (
	"bytes"
	"errors"
	"io"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/scarb/skope/internal/cli"
	"github.com/scarb/skope/internal/config"
)

func TestListReportsMissingConfiguration(t *testing.T) {
	var stdout, stderr bytes.Buffer
	app := cli.Application{
		LoadSkillSets: func() (string, config.SkillSets, error) {
			return "/home/test/.skope/skillsets.toml", config.SkillSets{Exists: false}, nil
		},
	}

	code := app.Execute([]string{"list"}, &stdout, &stderr, "test")

	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr=%q", code, stderr.String())
	}
	if got, want := stdout.String(), "/home/test/.skope/skillsets.toml\n暂无配置\n"; got != want {
		t.Errorf("stdout = %q, want %q", got, want)
	}
}

func TestListEscapesFieldsBeforeTabAlignment(t *testing.T) {
	app := cli.Application{LoadSkillSets: func() (string, config.SkillSets, error) {
		return "", config.SkillSets{Exists: true, Items: []config.SkillSet{{Name: "dev\x00\x7f", Description: "中\t文\u0085"}}}, nil
	}}
	var stdout, stderr bytes.Buffer
	code := app.Execute([]string{"list"}, &stdout, &stderr, "")
	if code != 0 || strings.Count(stdout.String(), "\n") != 2 || !strings.Contains(stdout.String(), `dev\x00\x7f  中\x09文\x85`) {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	assertNoTerminalControls(t, stdout.String())
}

func TestListPrintsSkillSetsInConfigurationOrder(t *testing.T) {
	var stdout, stderr bytes.Buffer
	app := cli.Application{
		LoadSkillSets: func() (string, config.SkillSets, error) {
			return "skillsets.toml", config.SkillSets{
				Exists: true,
				Items: []config.SkillSet{
					{
						Name:        "dev",
						Description: "daily work",
						Skills:      []string{"tdd", "debugging"},
						Plugins: map[string][]string{
							"claude": {"one@official", "two@official"},
							"codex":  {"three@official"},
						},
						Bundled: true,
					},
					{
						Name:        "empty",
						Description: "",
						Skills:      []string{},
						Plugins:     map[string][]string{},
						Bundled:     false,
					},
				},
			}, nil
		},
	}

	code := app.Execute([]string{"list"}, &stdout, &stderr, "test")

	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr=%q", code, stderr.String())
	}
	lines := strings.Split(strings.TrimSuffix(stdout.String(), "\n"), "\n")
	if len(lines) != 3 {
		t.Fatalf("output lines = %d, want 3:\n%s", len(lines), stdout.String())
	}
	if got, want := tableColumns(lines[0]), []string{"NAME", "DESCRIPTION", "SKILLS", "CLAUDE PLUGINS", "CODEX PLUGINS", "BUNDLED"}; !reflect.DeepEqual(got, want) {
		t.Errorf("header columns = %q, want %q", got, want)
	}
	if got := tableColumns(lines[1]); !reflect.DeepEqual(got, []string{"dev", "daily work", "2", "2", "1", "on"}) {
		t.Errorf("first row columns = %q", got)
	}
	if got := strings.Fields(lines[2]); !reflect.DeepEqual(got, []string{"empty", "0", "0", "0", "off"}) {
		t.Errorf("empty-description row fields = %q", got)
	}
	if strings.Index(stdout.String(), "dev") > strings.Index(stdout.String(), "empty") {
		t.Errorf("rows are not in configuration order:\n%s", stdout.String())
	}
}

func TestListReturnsOneWhenLoaderFails(t *testing.T) {
	var stdout, stderr bytes.Buffer
	app := cli.Application{
		LoadSkillSets: func() (string, config.SkillSets, error) {
			return "skillsets.toml", config.SkillSets{}, errors.New("broken skill sets")
		},
	}

	code := app.Execute([]string{"list"}, &stdout, &stderr, "test")

	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "broken skill sets") {
		t.Errorf("stderr = %q, want loader error", stderr.String())
	}
}

func TestListRejectsPositionalArguments(t *testing.T) {
	var stdout, stderr bytes.Buffer
	app := cli.Application{
		LoadSkillSets: func() (string, config.SkillSets, error) {
			t.Fatal("loader called for invalid positional argument")
			return "", config.SkillSets{}, nil
		},
	}

	code := app.Execute([]string{"list", "extra"}, &stdout, &stderr, "test")

	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "unknown command") && !strings.Contains(stderr.String(), "accepts 0 arg") {
		t.Errorf("stderr = %q, want positional argument error", stderr.String())
	}
}

func TestListWithoutLoaderReturnsInternalConfigurationError(t *testing.T) {
	var stdout, stderr bytes.Buffer

	code := (cli.Application{}).Execute([]string{"list"}, &stdout, &stderr, "test")

	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "skill set loader is not configured") {
		t.Errorf("stderr = %q, want internal configuration error", stderr.String())
	}
}

func TestListReturnsOneWhenMissingConfigurationWriteFails(t *testing.T) {
	var stderr bytes.Buffer
	app := cli.Application{
		LoadSkillSets: func() (string, config.SkillSets, error) {
			return "skillsets.toml", config.SkillSets{Exists: false}, nil
		},
	}

	code := app.Execute([]string{"list"}, failingWriter{}, &stderr, "test")

	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "write failed") {
		t.Errorf("stderr = %q, want write error", stderr.String())
	}
}

func TestListReturnsOneWhenTableFlushFails(t *testing.T) {
	var stderr bytes.Buffer
	app := cli.Application{
		LoadSkillSets: func() (string, config.SkillSets, error) {
			return "skillsets.toml", config.SkillSets{
				Exists: true,
				Items:  []config.SkillSet{{Name: "dev", Bundled: true}},
			}, nil
		},
	}

	code := app.Execute([]string{"list"}, failingWriter{}, &stderr, "test")

	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "write failed") {
		t.Errorf("stderr = %q, want flush error", stderr.String())
	}
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) {
	return 0, errors.New("write failed")
}

var _ io.Writer = failingWriter{}

func tableColumns(line string) []string {
	return regexp.MustCompile(` {2,}`).Split(strings.TrimSpace(line), -1)
}
