package cli_test

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"reflect"
	"strings"
	"testing"

	"github.com/scarb/skope/internal/cli"
	"github.com/scarb/skope/internal/launch"
	"github.com/scarb/skope/internal/testutil"
)

func TestHiddenCompletionRejectsTerminalControls(t *testing.T) {
	for _, name := range []string{"__complete", "__completeNoDesc"} {
		for _, prefix := range [][]string{nil, {"--help=false"}, {"-h=false"}} {
			for _, control := range []string{"\x00", "\x1b[31m", "\n", "\t", "\x7f", "\u009b", "\u061c", "\u200e", "\u200f", "\u2028", "\u2029", "\u202a", "\u202e", "\u2066", "\u2069"} {
				t.Run(fmt.Sprintf("%s/%v/%q", name, prefix, control), func(t *testing.T) {
					args := append(append([]string{}, prefix...), name, "--unknown"+control, "")
					var out, stderr bytes.Buffer
					code := (cli.Application{}).Execute(args, &out, &stderr, "")
					if code != 1 || out.Len() != 0 || strings.Count(stderr.String(), "Error:") != 1 {
						t.Fatalf("code=%d stdout=%q stderr=%q", code, out.String(), stderr.String())
					}
					assertNoTerminalControls(t, stderr.String())
				})
			}
		}
	}
}

func TestHiddenCompletionPreservesNormalProtocol(t *testing.T) {
	for _, name := range []string{"__complete", "__completeNoDesc"} {
		for _, prefix := range []string{"", "cla", "中文", `C:\普通路径\skill`} {
			var out, stderr bytes.Buffer
			code := (cli.Application{}).Execute([]string{name, prefix}, &out, &stderr, "")
			if code != 0 || !strings.Contains(out.String(), ":") || !strings.HasSuffix(out.String(), "\n") {
				t.Errorf("%s/%q code=%d output=%q stderr=%q", name, prefix, code, out.String(), stderr.String())
			}
			if prefix == "cla" && !strings.Contains(out.String(), "claude") {
				t.Errorf("missing completion: %q", out.String())
			}
		}
	}
}

func TestCompletionScriptsStillGenerateWithoutDependencies(t *testing.T) {
	for _, shell := range []string{"bash", "zsh", "fish", "powershell"} {
		var out, stderr bytes.Buffer
		code := (cli.Application{}).Execute([]string{"completion", shell}, &out, &stderr, "")
		if code != 0 || !strings.Contains(out.String(), "skope") || stderr.Len() != 0 {
			t.Errorf("%s code=%d output=%q stderr=%q", shell, code, out.String(), stderr.String())
		}
	}
}

func TestLaunchArgumentsNamedLikeCompletionRemainUnchanged(t *testing.T) {
	want := []string{"__complete", "--value\u202e"}
	var got []string
	app := cli.Application{RunLaunch: func(_ context.Context, request launch.Request, _ launch.Reporter) error {
		got = request.AgentArgs
		return nil
	}}
	var out, stderr bytes.Buffer
	code := app.Execute(append([]string{"claude", "-s", "none", "--"}, want...), &out, &stderr, "")
	if code != 0 || !reflect.DeepEqual(got, want) {
		t.Fatalf("code=%d got=%q stderr=%q", code, got, stderr.String())
	}
}

func TestHiddenCompletionBinaryCapturesGlobalStderr(t *testing.T) {
	if testing.Short() {
		t.Skip("builds skope")
	}
	binary := testutil.BuildSkope(t)
	for _, name := range []string{"__complete", "__completeNoDesc"} {
		for _, prefix := range [][]string{nil, {"--help=false"}, {"-h=false"}} {
			for _, suffix := range []string{"\u202e", "\x1b[31m", ""} {
				args := append(append([]string{}, prefix...), name, "--unknown"+suffix, "")
				cmd := exec.Command(binary, args...)
				var out, stderr bytes.Buffer
				cmd.Stdout, cmd.Stderr = &out, &stderr
				err := cmd.Run()
				if suffix == "" {
					if err != nil || !strings.Contains(out.String(), ":0\n") {
						t.Errorf("ASCII protocol: args=%q err=%v out=%q stderr=%q", args, err, out.String(), stderr.String())
					}
				} else if err == nil || out.Len() != 0 || strings.Count(stderr.String(), "Error:") != 1 {
					t.Errorf("unsafe request: args=%q err=%v out=%q stderr=%q", args, err, out.String(), stderr.String())
				}
				assertNoTerminalControls(t, stderr.String())
			}
		}
	}
}
