package cli

import (
	"errors"
	"strings"
)

var (
	errSetValueMissing = errors.New("set value is missing")
	errSetValueEmpty   = errors.New("set value is empty")
	errSetRepeated     = errors.New("set is repeated")
)

type parsedLaunchArgs struct {
	setValue   string
	setPresent bool
	dryRun     bool
	agentArgs  []string
}

func parseLaunchArgs(args []string) (parsedLaunchArgs, error) {
	var parsed parsedLaunchArgs

	for i := 0; i < len(args); i++ {
		token := args[i]
		switch {
		case token == "--":
			parsed.agentArgs = append([]string(nil), args[i+1:]...)
			return parsed, nil
		case token == "-s" || token == "--set":
			if parsed.setPresent {
				return parsedLaunchArgs{}, errSetRepeated
			}
			if i+1 == len(args) {
				return parsedLaunchArgs{}, errSetValueMissing
			}
			if args[i+1] == "" {
				return parsedLaunchArgs{}, errSetValueEmpty
			}
			parsed.setValue = args[i+1]
			parsed.setPresent = true
			i++
		case strings.HasPrefix(token, "-s=") || strings.HasPrefix(token, "--set="):
			if parsed.setPresent {
				return parsedLaunchArgs{}, errSetRepeated
			}
			_, value, _ := strings.Cut(token, "=")
			if value == "" {
				return parsedLaunchArgs{}, errSetValueEmpty
			}
			parsed.setValue = value
			parsed.setPresent = true
		case token == "--dry-run":
			parsed.dryRun = true
		default:
			parsed.agentArgs = append([]string(nil), args[i:]...)
			return parsed, nil
		}
	}

	return parsed, nil
}
