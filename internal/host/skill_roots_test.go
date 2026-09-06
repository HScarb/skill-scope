package host_test

import (
	"errors"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/scarb/skope/internal/host"
)

func TestResolveCodexPathsUsesPlatformHomeAndValidatesOverride(t *testing.T) {
	home := t.TempDir()
	for _, override := range []string{"", " \t ", "  " + filepath.Join(home, "custom") + "  "} {
		env := host.NewEnv("ignored", home, map[string]string{"CODEX_HOME": override, "HOME": "ignored", "USERPROFILE": "ignored"})
		before := env.Environ()
		got, err := host.ResolveCodexPathsForTest(env, func(host.Env) (string, []string, []string, error) {
			return home, []string{filepath.Join(home, "admin")}, []string{filepath.Join(home, "config")}, nil
		})
		want := filepath.Join(home, ".codex")
		if override != "" && override != " \t " {
			want = filepath.Join(home, "custom")
		}
		if err != nil || got.Home != home || got.CodexHome != want || !reflect.DeepEqual(before, env.Environ()) {
			t.Fatalf("%+v %v", got, err)
		}
	}
	for _, override := range []string{"relative", "../escape"} {
		if _, err := host.ResolveCodexPathsForTest(host.NewEnv(home, home, map[string]string{"CODEX_HOME": override}), func(host.Env) (string, []string, []string, error) { return home, nil, nil, nil }); err == nil {
			t.Fatal("relative override accepted")
		}
	}
	sentinel := errors.New("lookup failure")
	if _, err := host.ResolveCodexPathsForTest(host.NewEnv(home, home, nil), func(host.Env) (string, []string, []string, error) { return "", nil, nil, sentinel }); !errors.Is(err, sentinel) {
		t.Fatal(err)
	}
}

func TestResolveCodexPathsCopiesRootSlices(t *testing.T) {
	home := t.TempDir()
	admins := []string{filepath.Join(home, "admin")}
	configs := []string{filepath.Join(home, "config")}
	lookup := func(host.Env) (string, []string, []string, error) { return home, admins, configs, nil }
	first, err := host.ResolveCodexPathsForTest(host.NewEnv(home, home, nil), lookup)
	if err != nil {
		t.Fatal(err)
	}
	first.AdminSkillRoots[0] = "changed"
	first.SystemConfigPaths[0] = "changed"
	second, err := host.ResolveCodexPathsForTest(host.NewEnv(home, home, nil), lookup)
	if err != nil || second.AdminSkillRoots[0] == "changed" || second.SystemConfigPaths[0] == "changed" {
		t.Fatalf("%+v %v", second, err)
	}
}
