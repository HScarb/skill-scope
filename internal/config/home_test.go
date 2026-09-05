package config_test

import (
	"path/filepath"
	"testing"

	"github.com/scarb/skope/internal/config"
	"github.com/scarb/skope/internal/host"
)

func TestResolveHome(t *testing.T) {
	t.Parallel()

	userHome := t.TempDir()
	absoluteOverride := filepath.Join(t.TempDir(), "skope")

	tests := []struct {
		name     string
		override *string
		want     string
		wantErr  bool
	}{
		{
			name: "unset uses default",
			want: filepath.Join(userHome, ".skope"),
		},
		{
			name:     "blank uses default",
			override: stringPointer(" \t\r\n "),
			want:     filepath.Join(userHome, ".skope"),
		},
		{
			name:     "tilde expands to home",
			override: stringPointer("~"),
			want:     userHome,
		},
		{
			name:     "tilde slash expands to home",
			override: stringPointer("~/x"),
			want:     filepath.Join(userHome, "x"),
		},
		{
			name:     "tilde backslash expands to home",
			override: stringPointer(`~\x`),
			want:     filepath.Join(userHome, "x"),
		},
		{
			name:     "absolute override is accepted",
			override: &absoluteOverride,
			want:     filepath.Clean(absoluteOverride),
		},
		{
			name:     "relative override is rejected",
			override: stringPointer(filepath.Join("relative", "skope")),
			wantErr:  true,
		},
		{
			name:     "other user tilde is rejected",
			override: stringPointer("~other/x"),
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			vars := map[string]string{}
			if tt.override != nil {
				vars["SKOPE_HOME"] = *tt.override
			}
			env := host.NewEnv(userHome, t.TempDir(), vars)

			got, err := config.ResolveHome(env)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("ResolveHome() = %q, nil; want error", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("ResolveHome() error = %v", err)
			}
			if got != tt.want {
				t.Fatalf("ResolveHome() = %q, want %q", got, tt.want)
			}
		})
	}
}

func stringPointer(value string) *string {
	return &value
}
