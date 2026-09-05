package projection

import (
	"io/fs"
	"path/filepath"
	"strings"
)

// TargetNamesConflict compares validated individual destination components.
func TargetNamesConflict(left, right string) bool { return strings.EqualFold(left, right) }

func validComponent(name string) bool {
	return name != "." && !strings.ContainsAny(name, "/\\:") && validPath(name)
}

func validPath(name string) bool {
	return fs.ValidPath(name) && filepath.IsLocal(filepath.FromSlash(name)) && !strings.ContainsAny(name, "\\:")
}
