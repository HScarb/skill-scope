package host_test

import (
	"reflect"
	"testing"

	"github.com/scarb/skope/internal/host"
)

func TestNewEnvCopiesInputAndReturnsSortedEnviron(t *testing.T) {
	vars := map[string]string{"Z": "last", "A": "first"}
	env := host.NewEnv("/home/me", "/repo", vars)
	vars["A"] = "mutated"

	if got := env.Get("A"); got != "first" {
		t.Fatalf("Get(A) = %q, want first", got)
	}
	if got := env.Environ(); !reflect.DeepEqual(got, []string{"A=first", "Z=last"}) {
		t.Fatalf("Environ() = %#v", got)
	}
	if env.Home() != "/home/me" || env.Cwd() != "/repo" {
		t.Fatalf("Home/Cwd = %q/%q", env.Home(), env.Cwd())
	}
}

func TestWithReturnsCopy(t *testing.T) {
	base := host.NewEnv("/home/me", "/repo", map[string]string{"A": "one"})
	next := base.With(map[string]string{"A": "two", "B": "three"})
	if base.Get("A") != "one" || next.Get("A") != "two" || next.Get("B") != "three" {
		t.Fatalf("base=%v next=%v", base.Environ(), next.Environ())
	}
}
