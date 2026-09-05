//go:build windows

package handoff

func (Handoff) Exec(string, []string, []string) error {
	return ErrUnsupported
}
