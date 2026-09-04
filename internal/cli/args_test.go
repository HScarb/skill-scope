package cli

import (
	"errors"
	"reflect"
	"testing"
)

func TestParseLaunchArgs(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want parsedLaunchArgs
		err  error
	}{
		{
			name: "short set",
			args: []string{"-s", "dev"},
			want: parsedLaunchArgs{setValue: "dev", setPresent: true},
		},
		{
			name: "long set",
			args: []string{"--set", "dev"},
			want: parsedLaunchArgs{setValue: "dev", setPresent: true},
		},
		{
			name: "long set equals",
			args: []string{"--set=dev"},
			want: parsedLaunchArgs{setValue: "dev", setPresent: true},
		},
		{
			name: "short set equals",
			args: []string{"-s=dev"},
			want: parsedLaunchArgs{setValue: "dev", setPresent: true},
		},
		{
			name: "dry run and set",
			args: []string{"--dry-run", "-s", "dev"},
			want: parsedLaunchArgs{setValue: "dev", setPresent: true, dryRun: true},
		},
		{
			name: "repeated dry run is idempotent",
			args: []string{"--dry-run", "--dry-run"},
			want: parsedLaunchArgs{dryRun: true},
		},
		{
			name: "separator starts passthrough",
			args: []string{"-s", "dev", "--", "--model", "x"},
			want: parsedLaunchArgs{
				setValue:   "dev",
				setPresent: true,
				agentArgs:  []string{"--model", "x"},
			},
		},
		{
			name: "unknown token starts passthrough",
			args: []string{"-s", "dev", "--model", "x", "-s", "other"},
			want: parsedLaunchArgs{
				setValue:   "dev",
				setPresent: true,
				agentArgs:  []string{"--model", "x", "-s", "other"},
			},
		},
		{
			name: "prompt starts passthrough",
			args: []string{"prompt", "text"},
			want: parsedLaunchArgs{agentArgs: []string{"prompt", "text"}},
		},
		{
			name: "dry run with value is unknown",
			args: []string{"--dry-run=x", "-s", "dev"},
			want: parsedLaunchArgs{agentArgs: []string{"--dry-run=x", "-s", "dev"}},
		},
		{
			name: "missing short set value",
			args: []string{"-s"},
			err:  errSetValueMissing,
		},
		{
			name: "missing long set value",
			args: []string{"--set"},
			err:  errSetValueMissing,
		},
		{
			name: "empty short set value",
			args: []string{"-s", ""},
			err:  errSetValueEmpty,
		},
		{
			name: "empty long set value",
			args: []string{"--set="},
			err:  errSetValueEmpty,
		},
		{
			name: "repeated set",
			args: []string{"-s", "dev", "--set", "other"},
			err:  errSetRepeated,
		},
		{
			name: "repeated set equals",
			args: []string{"--set=dev", "-s=other"},
			err:  errSetRepeated,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			before := append([]string(nil), tt.args...)

			got, err := parseLaunchArgs(tt.args)

			if !errors.Is(err, tt.err) {
				t.Fatalf("parseLaunchArgs() error = %v, want errors.Is(%v)", err, tt.err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("parseLaunchArgs() = %#v, want %#v", got, tt.want)
			}
			if !reflect.DeepEqual(tt.args, before) {
				t.Errorf("parseLaunchArgs() changed input to %#v, want %#v", tt.args, before)
			}
		})
	}
}

func TestParseLaunchArgsAgentArgsDoesNotAliasInput(t *testing.T) {
	args := []string{"prompt", "text"}

	got, err := parseLaunchArgs(args)
	if err != nil {
		t.Fatalf("parseLaunchArgs() error = %v", err)
	}

	got.agentArgs[0] = "changed"
	if args[0] != "prompt" {
		t.Fatalf("input changed through result: args[0] = %q, want prompt", args[0])
	}
}
