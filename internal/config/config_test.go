package config_test

import (
	"errors"
	"io/fs"
	"reflect"
	"strings"
	"testing"

	"github.com/scarb/skope/internal/config"
)

func TestLoadMissingFileReturnsEmptyConfig(t *testing.T) {
	t.Parallel()

	got, err := config.Load(readFileFS{err: fs.ErrNotExist}, "config.toml")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if got.Exists {
		t.Fatal("Load() Exists = true, want false")
	}
	if got.Agents == nil || len(got.Agents) != 0 {
		t.Fatalf("Load() Agents = %#v, want non-nil empty map", got.Agents)
	}
}

func TestLoadReadErrorFailsClosed(t *testing.T) {
	t.Parallel()

	path := "custom/config.toml"
	cause := errors.New("read failed")
	got, err := config.Load(readFileFS{err: cause}, path)
	if err == nil {
		t.Fatal("Load() error = nil, want read error")
	}
	var pathErr *config.PathError
	if !errors.As(err, &pathErr) {
		t.Fatalf("Load() error = %T %v, want *config.PathError", err, err)
	}
	if pathErr.Path != path {
		t.Fatalf("Load() error path = %q, want %q", pathErr.Path, path)
	}
	if !errors.Is(err, cause) {
		t.Fatalf("errors.Is(%v, cause) = false, want true", err)
	}
	if got.Exists || got.Agents != nil {
		t.Fatalf("Load() config = %#v, want unusable zero value", got)
	}
}

func TestLoadValidConfig(t *testing.T) {
	t.Parallel()

	data := []byte(`
version = 1

[agents.claude]
command = "claude"
args = ["--debug", "value"]

[agents.codex]
command = "codex"
args = ["exec"]

[agents.opencode]
command = "opencode"
args = []
`)
	want := config.Config{
		Exists: true,
		Agents: map[string]config.AgentConfig{
			"claude": {
				Command: "claude",
				Args:    []string{"--debug", "value"},
			},
			"codex": {
				Command: "codex",
				Args:    []string{"exec"},
			},
			"opencode": {
				Command: "opencode",
				Args:    []string{},
			},
		},
	}

	got, err := config.Load(readFileFS{data: data}, "config.toml")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Load() = %#v, want %#v", got, want)
	}
}

func TestLoadValidSparseConfig(t *testing.T) {
	t.Parallel()

	got, err := config.Load(readFileFS{data: []byte("version = 1\n")}, "config.toml")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if !got.Exists {
		t.Fatal("Load() Exists = false, want true")
	}
	if got.Agents == nil || len(got.Agents) != 0 {
		t.Fatalf("Load() Agents = %#v, want non-nil empty map", got.Agents)
	}
}

func TestLoadRejectsInvalidVersion(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		data string
	}{
		{
			name: "missing",
			data: "[agents]",
		},
		{
			name: "unsupported",
			data: "version = 2",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := config.Load(readFileFS{data: []byte(tt.data)}, "config.toml")
			if err == nil {
				t.Fatal("Load() error = nil, want version error")
			}
			var pathErr *config.PathError
			if !errors.As(err, &pathErr) {
				t.Fatalf("Load() error = %T %v, want *config.PathError", err, err)
			}
			if pathErr.Field != "version" {
				t.Fatalf("Load() error field = %q, want version", pathErr.Field)
			}
		})
	}
}

func TestLoadRejectsUnknownFields(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		data      string
		wantField string
	}{
		{
			name:      "root",
			data:      "version = 1\nextra = true",
			wantField: "extra",
		},
		{
			name:      "agent",
			data:      "version = 1\n[agents.other]\ncommand = 'other'",
			wantField: "agents.other",
		},
		{
			name:      "agent field",
			data:      "version = 1\n[agents.claude]\ntimeout = 1",
			wantField: "agents.claude.timeout",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := config.Load(readFileFS{data: []byte(tt.data)}, "config.toml")
			if err == nil {
				t.Fatalf("Load() error = nil, want error containing %q", tt.wantField)
			}
			var pathErr *config.PathError
			if !errors.As(err, &pathErr) {
				t.Fatalf("Load() error = %T %v, want *config.PathError", err, err)
			}
			if pathErr.Field != tt.wantField {
				t.Fatalf("Load() error field = %q, want %q", pathErr.Field, tt.wantField)
			}
			if !strings.Contains(err.Error(), tt.wantField) {
				t.Fatalf("Load() error = %q, want field %q", err, tt.wantField)
			}
		})
	}
}

func TestLoadRejectsWrongTypes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		data string
	}{
		{
			name: "command",
			data: "version = 1\n[agents.claude]\ncommand = []",
		},
		{
			name: "args member",
			data: "version = 1\n[agents.claude]\nargs = [1]",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := config.Load(readFileFS{data: []byte(tt.data)}, "config.toml")
			if err == nil {
				t.Fatal("Load() error = nil, want decode error")
			}
			var pathErr *config.PathError
			if !errors.As(err, &pathErr) {
				t.Fatalf("Load() error = %T %v, want *config.PathError", err, err)
			}
		})
	}
}

func TestPathErrorFormatsAndUnwraps(t *testing.T) {
	t.Parallel()

	cause := errors.New("invalid value")
	tests := []struct {
		name string
		err  *config.PathError
		want string
	}{
		{
			name: "path only",
			err:  &config.PathError{Path: "config.toml", Err: cause},
			want: "config.toml: invalid value",
		},
		{
			name: "path and field",
			err:  &config.PathError{Path: "config.toml", Field: "version", Err: cause},
			want: "config.toml: version: invalid value",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := tt.err.Error(); got != tt.want {
				t.Fatalf("PathError.Error() = %q, want %q", got, tt.want)
			}
			if !errors.Is(tt.err, cause) {
				t.Fatalf("errors.Is(%v, cause) = false, want true", tt.err)
			}
		})
	}
}

type readFileFS struct {
	data []byte
	err  error
}

func (f readFileFS) ReadFile(string) ([]byte, error) {
	return f.data, f.err
}
