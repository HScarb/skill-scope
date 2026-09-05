package host

import "os"

func openRegular(name string) (*os.File, error) {
	return os.Open(name)
}
