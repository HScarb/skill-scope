package config_test

import (
	"errors"
	"io/fs"
	"reflect"
	"strings"
	"testing"

	"github.com/scarb/skope/internal/config"
)

func TestLoadSkillSetsMissingFileReturnsEmpty(t *testing.T) {
	t.Parallel()

	got, err := config.LoadSkillSets(readFileFS{err: fs.ErrNotExist}, "skillsets.toml")
	if err != nil {
		t.Fatalf("LoadSkillSets() error = %v", err)
	}
	if got.Exists {
		t.Fatal("LoadSkillSets() Exists = true, want false")
	}
	if got.Items == nil || len(got.Items) != 0 {
		t.Fatalf("LoadSkillSets() Items = %#v, want non-nil empty slice", got.Items)
	}
}

func TestLoadSkillSetsReadErrorFailsClosed(t *testing.T) {
	t.Parallel()

	cause := errors.New("read failed")
	_, err := config.LoadSkillSets(readFileFS{err: cause}, "custom/skillsets.toml")
	if err == nil {
		t.Fatal("LoadSkillSets() error = nil, want read error")
	}
	var pathErr *config.PathError
	if !errors.As(err, &pathErr) || pathErr.Path != "custom/skillsets.toml" {
		t.Fatalf("LoadSkillSets() error = %T %v, want PathError for input path", err, err)
	}
	if !errors.Is(err, cause) {
		t.Fatalf("errors.Is(%v, cause) = false, want true", err)
	}
}

func TestLoadSkillSetsValidatesVersionFieldsAndTypes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		data      string
		wantField string
	}{
		{name: "missing version", data: `[skillsets.dev]
skills = []`, wantField: "version"},
		{name: "unsupported version", data: `version = 2`, wantField: "version"},
		{name: "unknown root field", data: `version = 1
extra = true`, wantField: "extra"},
		{name: "unknown set field", data: `version = 1
[skillsets.dev]
skills = []
extra = true`, wantField: "skillsets.dev.extra"},
		{name: "unknown plugin field", data: `version = 1
[skillsets.dev]
skills = []
[skillsets.dev.plugins]
other = []`, wantField: "skillsets.dev.plugins.other"},
		{name: "opencode plugin field", data: `version = 1
[skillsets.dev]
skills = []
[skillsets.dev.plugins]
opencode = []`, wantField: "skillsets.dev.plugins.opencode"},
		{name: "version type", data: `version = "1"`, wantField: "version"},
		{name: "skillsets type", data: `version = 1
skillsets = []`, wantField: "skillsets"},
		{name: "description type", data: `version = 1
[skillsets.dev]
skills = []
description = 1`, wantField: "skillsets.dev.description"},
		{name: "skills type", data: `version = 1
[skillsets.dev]
skills = "commit"`, wantField: "skillsets.dev.skills"},
		{name: "skills member type", data: `version = 1
[skillsets.dev]
skills = [1]`, wantField: "skillsets.dev.skills"},
		{name: "plugins type", data: `version = 1
[skillsets.dev]
skills = []
plugins = []`, wantField: "skillsets.dev.plugins"},
		{name: "plugin list type", data: `version = 1
[skillsets.dev]
skills = []
[skillsets.dev.plugins]
claude = "x@y"`, wantField: "skillsets.dev.plugins.claude"},
		{name: "plugin member type", data: `version = 1
[skillsets.dev]
skills = []
[skillsets.dev.plugins]
codex = [1]`, wantField: "skillsets.dev.plugins.codex"},
		{name: "bundled type", data: `version = 1
[skillsets.dev]
skills = []
bundled = "false"`, wantField: "skillsets.dev.bundled"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := config.LoadSkillSets(readFileFS{data: []byte(tt.data)}, "skillsets.toml")
			if err == nil {
				t.Fatalf("LoadSkillSets() error = nil, want error for %s", tt.wantField)
			}
			var pathErr *config.PathError
			if !errors.As(err, &pathErr) {
				t.Fatalf("LoadSkillSets() error = %T %v, want *config.PathError", err, err)
			}
			if pathErr.Field != tt.wantField {
				t.Fatalf("LoadSkillSets() error field = %q, want %q; error = %v", pathErr.Field, tt.wantField, err)
			}
		})
	}
}

func TestLoadSkillSetsValidatesSetNames(t *testing.T) {
	t.Parallel()

	invalid := []string{"", "none", "-dev", "dev set", "dev,test", "dev@work"}
	for _, name := range invalid {
		name := name
		t.Run("invalid "+name, func(t *testing.T) {
			t.Parallel()

			data := "version = 1\n[skillsets." + quoteTOMLKey(name) + "]\nskills = []"
			_, err := config.LoadSkillSets(readFileFS{data: []byte(data)}, "skillsets.toml")
			if err == nil {
				t.Fatalf("LoadSkillSets() accepted set name %q", name)
			}
		})
	}

	for _, name := range []string{"dev", "foo.bar", "a_b-c9"} {
		name := name
		t.Run("valid "+name, func(t *testing.T) {
			t.Parallel()

			data := "version = 1\n[skillsets." + quoteTOMLKey(name) + "]\nskills = []"
			got, err := config.LoadSkillSets(readFileFS{data: []byte(data)}, "skillsets.toml")
			if err != nil {
				t.Fatalf("LoadSkillSets() error = %v", err)
			}
			if len(got.Items) != 1 || got.Items[0].Name != name {
				t.Fatalf("LoadSkillSets() Items = %#v, want set %q", got.Items, name)
			}
		})
	}
}

func TestLoadSkillSetsReportsEmptySetNameAsValidationError(t *testing.T) {
	t.Parallel()

	data := `version = 1
[skillsets.""]
skills = []`
	_, err := config.LoadSkillSets(readFileFS{data: []byte(data)}, "skillsets.toml")
	if err == nil {
		t.Fatal("LoadSkillSets() error = nil, want empty set name error")
	}
	var pathErr *config.PathError
	if !errors.As(err, &pathErr) {
		t.Fatalf("LoadSkillSets() error = %T %v, want *config.PathError", err, err)
	}
	if pathErr.Field != "skillsets." {
		t.Fatalf("LoadSkillSets() error field = %q, want skillsets.", pathErr.Field)
	}
	if strings.Contains(err.Error(), "internal invariant") {
		t.Fatalf("LoadSkillSets() error = %q, want user validation error", err)
	}
}

func TestLoadSkillSetsValidatesAndNormalizesSkills(t *testing.T) {
	t.Parallel()

	invalid := []struct {
		name string
		body string
	}{
		{name: "missing", body: ""},
		{name: "empty member", body: `skills = ["commit", "  "]`},
		{name: "duplicate after trim", body: `skills = ["commit", " commit "]`},
	}
	for _, tt := range invalid {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			data := "version = 1\n[skillsets.dev]\n" + tt.body
			_, err := config.LoadSkillSets(readFileFS{data: []byte(data)}, "skillsets.toml")
			if err == nil {
				t.Fatal("LoadSkillSets() error = nil, want skills validation error")
			}
			var pathErr *config.PathError
			if !errors.As(err, &pathErr) || pathErr.Field != "skillsets.dev.skills" {
				t.Fatalf("LoadSkillSets() error = %T %v, want skills PathError", err, err)
			}
		})
	}

	data := `version = 1
[skillsets.dev]
skills = [" commit ", "apps/web:verify"]`
	got, err := config.LoadSkillSets(readFileFS{data: []byte(data)}, "skillsets.toml")
	if err != nil {
		t.Fatalf("LoadSkillSets() error = %v", err)
	}
	if want := []string{"commit", "apps/web:verify"}; !reflect.DeepEqual(got.Items[0].Skills, want) {
		t.Fatalf("LoadSkillSets() Skills = %#v, want %#v", got.Items[0].Skills, want)
	}
}

func TestLoadSkillSetsValidatesAndNormalizesPlugins(t *testing.T) {
	t.Parallel()

	invalid := []struct {
		name string
		id   string
	}{
		{name: "missing separator", id: "plugin"},
		{name: "empty name", id: "@market"},
		{name: "empty marketplace", id: "plugin@"},
		{name: "multiple separators", id: "plugin@market@extra"},
	}
	for _, tt := range invalid {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			data := "version = 1\n[skillsets.dev]\nskills = []\n[skillsets.dev.plugins]\nclaude = [\"" + tt.id + "\"]"
			_, err := config.LoadSkillSets(readFileFS{data: []byte(data)}, "skillsets.toml")
			if err == nil {
				t.Fatalf("LoadSkillSets() accepted plugin ID %q", tt.id)
			}
		})
	}

	duplicate := `version = 1
[skillsets.dev]
skills = []
[skillsets.dev.plugins]
codex = ["plugin@market", " plugin@market "]`
	_, err := config.LoadSkillSets(readFileFS{data: []byte(duplicate)}, "skillsets.toml")
	if err == nil {
		t.Fatal("LoadSkillSets() accepted duplicate plugin IDs after trim")
	}

	valid := `version = 1
[skillsets.dev]
skills = []
[skillsets.dev.plugins]
claude = [" first@market "]
codex = ["second@other"]`
	got, err := config.LoadSkillSets(readFileFS{data: []byte(valid)}, "skillsets.toml")
	if err != nil {
		t.Fatalf("LoadSkillSets() error = %v", err)
	}
	want := map[string][]string{"claude": {"first@market"}, "codex": {"second@other"}}
	if !reflect.DeepEqual(got.Items[0].Plugins, want) {
		t.Fatalf("LoadSkillSets() Plugins = %#v, want %#v", got.Items[0].Plugins, want)
	}
}

func TestLoadSkillSetsValidatesPluginAgentsInFixedOrder(t *testing.T) {
	t.Parallel()

	data := `version = 1
[skillsets.dev]
skills = []
[skillsets.dev.plugins]
claude = [""]
codex = [""]`
	for range 100 {
		_, err := config.LoadSkillSets(readFileFS{data: []byte(data)}, "skillsets.toml")
		var pathErr *config.PathError
		if !errors.As(err, &pathErr) {
			t.Fatalf("LoadSkillSets() error = %T %v, want *config.PathError", err, err)
		}
		if pathErr.Field != "skillsets.dev.plugins.claude" {
			t.Fatalf("LoadSkillSets() error field = %q, want deterministic Claude-first validation", pathErr.Field)
		}
	}
}

func TestLoadSkillSetsDefaultsAndPreservesOptionalFields(t *testing.T) {
	t.Parallel()

	data := `version = 1
[skillsets.default]
skills = []

[skillsets.minimal]
description = "small"
skills = []
bundled = false`
	got, err := config.LoadSkillSets(readFileFS{data: []byte(data)}, "skillsets.toml")
	if err != nil {
		t.Fatalf("LoadSkillSets() error = %v", err)
	}
	if got.Items[0].Description != "" || !got.Items[0].Bundled {
		t.Fatalf("default set = %#v, want empty description and bundled=true", got.Items[0])
	}
	if got.Items[1].Description != "small" || got.Items[1].Bundled {
		t.Fatalf("minimal set = %#v, want description small and bundled=false", got.Items[1])
	}
}

func TestLoadSkillSetsPreservesConfigurationOrder(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		data string
		want []string
	}{
		{
			name: "table order",
			data: `version = 1
[skillsets.second]
skills = []
[skillsets.first]
skills = []`,
			want: []string{"second", "first"},
		},
		{
			name: "quoted dotted name",
			data: `version = 1
[skillsets."foo.bar"]
skills = []`,
			want: []string{"foo.bar"},
		},
		{
			name: "dotted keys",
			data: `version = 1
skillsets.second.skills = []
skillsets.first.skills = []`,
			want: []string{"second", "first"},
		},
		{
			name: "relative inline keys under skillsets table",
			data: `version = 1
[skillsets]
second = { skills = [], plugins = { claude = ["sample@market"] } }
"foo.bar" = { skills = [] }`,
			want: []string{"second", "foo.bar"},
		},
		{
			name: "relative dotted keys under skillsets table",
			data: `version = 1
[skillsets]
second.skills = []
first.skills = []`,
			want: []string{"second", "first"},
		},
		{
			name: "inline table skips comment nodes",
			data: `version = 1
skillsets = { # comment
 dev = { skills = [] } }`,
			want: []string{"dev"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := config.LoadSkillSets(readFileFS{data: []byte(tt.data)}, "skillsets.toml")
			if err != nil {
				t.Fatalf("LoadSkillSets() error = %v", err)
			}
			if names := got.Names(); !reflect.DeepEqual(names, tt.want) {
				t.Fatalf("Names() = %#v, want %#v", names, tt.want)
			}
		})
	}
}

func TestParseSelection(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		value   string
		want    []string
		wantErr bool
	}{
		{name: "single", value: "dev", want: []string{"dev"}},
		{name: "trim and stable deduplicate", value: " second, first,second ", want: []string{"second", "first"}},
		{name: "none", value: " none ", want: []string{"none"}},
		{name: "duplicate none", value: "none,none", want: []string{"none"}},
		{name: "empty", value: "", wantErr: true},
		{name: "empty item", value: "dev,,test", wantErr: true},
		{name: "none first", value: "none,dev", wantErr: true},
		{name: "none last", value: "dev,none", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := config.ParseSelection(tt.value)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("ParseSelection(%q) error = nil, want error", tt.value)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseSelection(%q) error = %v", tt.value, err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("ParseSelection(%q) = %#v, want %#v", tt.value, got, tt.want)
			}
		})
	}
}

func TestMergeReturnsStableUnionWithoutMutatingSource(t *testing.T) {
	t.Parallel()

	sets := config.SkillSets{Exists: true, Items: []config.SkillSet{
		{Name: "first", Skills: []string{"a", "shared"}, Plugins: map[string][]string{"claude": {"a@m", "shared@m"}}, Bundled: false},
		{Name: "second", Skills: []string{"shared", "b"}, Plugins: map[string][]string{"claude": {"shared@m"}, "codex": {"b@m"}}, Bundled: true},
	}}
	names := []string{"first", "second"}
	wantNames := append([]string{}, names...)

	got, err := sets.Merge(names)
	if err != nil {
		t.Fatalf("Merge() error = %v", err)
	}
	want := config.Selection{
		DisplayName: "first+second",
		Skills:      []string{"a", "shared", "b"},
		Plugins:     map[string][]string{"claude": {"a@m", "shared@m"}, "codex": {"b@m"}},
		Bundled:     true,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Merge() = %#v, want %#v", got, want)
	}
	if !reflect.DeepEqual(names, wantNames) {
		t.Fatalf("Merge() mutated names: got %#v, want %#v", names, wantNames)
	}

	got.Skills[0] = "changed"
	got.Plugins["claude"][0] = "changed@m"
	if sets.Items[0].Skills[0] != "a" || sets.Items[0].Plugins["claude"][0] != "a@m" {
		t.Fatalf("Merge() result aliases source: %#v", sets.Items[0])
	}
}

func TestMergeUnknownSetReturnsOrderedAvailableNames(t *testing.T) {
	t.Parallel()

	sets := config.SkillSets{Items: []config.SkillSet{{Name: "second"}, {Name: "first"}}}
	_, err := sets.Merge([]string{"missing"})
	if err == nil {
		t.Fatal("Merge() error = nil, want unknown set error")
	}
	var unknown *config.UnknownSetError
	if !errors.As(err, &unknown) {
		t.Fatalf("Merge() error = %T %v, want *config.UnknownSetError", err, err)
	}
	if unknown.Name != "missing" || !reflect.DeepEqual(unknown.Available, []string{"second", "first"}) {
		t.Fatalf("UnknownSetError = %#v, want missing and ordered available names", unknown)
	}
}

func TestNamesReturnsCopy(t *testing.T) {
	t.Parallel()

	sets := config.SkillSets{Items: []config.SkillSet{{Name: "dev"}}}
	got := sets.Names()
	got[0] = "changed"
	if sets.Items[0].Name != "dev" {
		t.Fatalf("Names() result aliases source: %#v", sets.Items)
	}
}

func quoteTOMLKey(value string) string {
	return `"` + strings.ReplaceAll(value, `"`, `\"`) + `"`
}
