package config

import (
	"fmt"
	"regexp"
	"strings"
)

var setNamePattern = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

func validateSetName(name string) error {
	if strings.TrimSpace(name) == "" {
		return fmt.Errorf("must not be empty")
	}
	if name == "none" {
		return fmt.Errorf("%q is reserved", name)
	}
	if strings.HasPrefix(name, "-") {
		return fmt.Errorf("must not start with a hyphen")
	}
	if strings.Contains(name, ",") {
		return fmt.Errorf("must not contain a comma")
	}
	if !setNamePattern.MatchString(name) {
		return fmt.Errorf("contains invalid characters")
	}
	return nil
}

func normalizeUnique(field string, values []string) ([]string, error) {
	result := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		normalized := strings.TrimSpace(value)
		if normalized == "" {
			return nil, fmt.Errorf("%s contains an empty value", field)
		}
		if _, exists := seen[normalized]; exists {
			return nil, fmt.Errorf("%s contains duplicate value %q", field, normalized)
		}
		seen[normalized] = struct{}{}
		result = append(result, normalized)
	}
	return result, nil
}

func validatePluginID(id string) error {
	if strings.Count(id, "@") != 1 {
		return fmt.Errorf("plugin ID %q must contain exactly one @", id)
	}
	name, marketplace, _ := strings.Cut(id, "@")
	if name == "" || marketplace == "" {
		return fmt.Errorf("plugin ID %q must have a name and marketplace", id)
	}
	return nil
}
