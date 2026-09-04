package host

import (
	"os"
	"sort"
	"strings"
)

// Env is an immutable snapshot of the host environment.
type Env struct {
	home string
	cwd  string
	vars map[string]string
}

// Snapshot reads the current host environment.
func Snapshot() (Env, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return Env{}, err
	}

	cwd, err := os.Getwd()
	if err != nil {
		return Env{}, err
	}

	vars := make(map[string]string)
	for _, entry := range os.Environ() {
		name, value, ok := strings.Cut(entry, "=")
		if !ok || name == "" {
			continue
		}
		vars[name] = value
	}

	return NewEnv(home, cwd, vars), nil
}

// NewEnv creates an environment snapshot from the supplied values.
func NewEnv(home, cwd string, vars map[string]string) Env {
	return Env{
		home: home,
		cwd:  cwd,
		vars: copyVars(vars),
	}
}

// Home returns the user home directory in the snapshot.
func (e Env) Home() string {
	return e.home
}

// Cwd returns the working directory in the snapshot.
func (e Env) Cwd() string {
	return e.cwd
}

// Get returns the value associated with name or an empty string.
func (e Env) Get(name string) string {
	return e.vars[name]
}

// Lookup returns the value associated with name and whether it exists.
func (e Env) Lookup(name string) (string, bool) {
	value, ok := e.vars[name]
	return value, ok
}

// Environ returns a sorted copy of the snapshot in key=value form.
func (e Env) Environ() []string {
	keys := make([]string, 0, len(e.vars))
	for key := range e.vars {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	environ := make([]string, 0, len(keys))
	for _, key := range keys {
		environ = append(environ, key+"="+e.vars[key])
	}
	return environ
}

// With returns a copy of the snapshot with overrides applied.
func (e Env) With(overrides map[string]string) Env {
	vars := copyVars(e.vars)
	for name, value := range overrides {
		vars[name] = value
	}
	return Env{home: e.home, cwd: e.cwd, vars: vars}
}

func copyVars(vars map[string]string) map[string]string {
	cloned := make(map[string]string, len(vars))
	for name, value := range vars {
		cloned[name] = value
	}
	return cloned
}
