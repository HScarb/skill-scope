package cli

import (
	"errors"
	"fmt"
	"path/filepath"
	"text/tabwriter"

	"github.com/scarb/skope/internal/config"
	"github.com/scarb/skope/internal/host"
	"github.com/scarb/skope/internal/termsafe"
	"github.com/spf13/cobra"
)

type listLoader func() (path string, sets config.SkillSets, err error)

func newListCmd(load listLoader) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List configured skill sets",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if load == nil {
				return errors.New("internal configuration error: skill set loader is not configured")
			}

			path, sets, err := load()
			if err != nil {
				return err
			}
			if !sets.Exists {
				_, err := fmt.Fprintf(cmd.OutOrStdout(), "%s\n暂无配置\n", termsafe.Escape(path))
				return err
			}

			writer := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 8, 2, ' ', 0)
			if _, err := fmt.Fprintln(writer, "NAME\tDESCRIPTION\tSKILLS\tCLAUDE PLUGINS\tCODEX PLUGINS\tBUNDLED"); err != nil {
				return err
			}
			for _, set := range sets.Items {
				bundled := "off"
				if set.Bundled {
					bundled = "on"
				}
				if _, err := fmt.Fprintf(writer, "%s\t%s\t%d\t%d\t%d\t%s\n",
					termsafe.Escape(set.Name),
					termsafe.Escape(set.Description),
					len(set.Skills),
					len(set.Plugins["claude"]),
					len(set.Plugins["codex"]),
					bundled,
				); err != nil {
					return err
				}
			}
			return writer.Flush()
		},
	}
}

func loadSkillSets() (string, config.SkillSets, error) {
	env, err := host.Snapshot()
	if err != nil {
		return "", config.SkillSets{}, err
	}
	home, err := config.ResolveHome(env)
	if err != nil {
		return "", config.SkillSets{}, err
	}

	path := filepath.Join(home, "skillsets.toml")
	sets, err := config.LoadSkillSets(host.OSFileSystem{}, path)
	return path, sets, err
}
