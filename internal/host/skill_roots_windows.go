package host

import "golang.org/x/sys/windows"

func codexPlatformPaths(_ Env) (string, []string, []string, error) {
	home, err := windows.KnownFolderPath(windows.FOLDERID_Profile, 0)
	return home, nil, nil, err
}
