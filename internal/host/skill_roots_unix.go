//go:build unix

package host

func codexPlatformPaths(env Env) (string, []string, []string, error) {
	return env.Home(), []string{"/etc/codex/skills"}, []string{"/etc/codex/config.toml"}, nil
}
