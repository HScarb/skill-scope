//go:build windows

package session

func (OSProcessInspector) StartToken(int) (string, error) {
	return "", ErrUnsupported
}
