package host

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"golang.org/x/sys/windows"
)

func openProjectionNode(root *os.Root, name string) (*os.File, error) { return root.Open(name) }

func projectionLinkLoop(err error) bool {
	return errors.Is(err, windows.ERROR_CANT_RESOLVE_FILENAME) || errors.Is(err, syscall.ELOOP)
}

// Go 1.24's winsymlink compatibility mode hides junctions from EvalSymlinks.
// Read reparse metadata explicitly; never open a target's contents to resolve it.
func evalProjectionLinks(name string) (string, error) {
	absolute, err := filepath.Abs(name)
	if err != nil {
		return "", err
	}
nextPath:
	for links := 0; links < 255; links++ {
		volume := filepath.VolumeName(absolute)
		current := volume + string(filepath.Separator)
		parts := strings.Split(strings.TrimPrefix(absolute[len(volume):], string(filepath.Separator)), string(filepath.Separator))
		for index, part := range parts {
			current = filepath.Join(current, part)
			info, err := os.Lstat(current)
			if err != nil {
				return "", err
			}
			attributes, ok := info.Sys().(*syscall.Win32FileAttributeData)
			if !ok || attributes.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT == 0 {
				continue
			}
			target, err := os.Readlink(current)
			if err != nil {
				return "", err
			}
			if !filepath.IsAbs(target) {
				target = filepath.Join(filepath.Dir(current), target)
			}
			absolute = filepath.Join(append([]string{target}, parts[index+1:]...)...)
			continue nextPath
		}
		return filepath.EvalSymlinks(absolute)
	}
	return "", syscall.ELOOP
}
