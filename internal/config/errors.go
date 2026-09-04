package config

import "fmt"

// PathError reports an error associated with a path and optional field.
type PathError struct {
	Path  string
	Field string
	Err   error
}

func (e *PathError) Error() string {
	if e.Field == "" {
		return fmt.Sprintf("%s: %v", e.Path, e.Err)
	}
	return fmt.Sprintf("%s: %s: %v", e.Path, e.Field, e.Err)
}

func (e *PathError) Unwrap() error {
	return e.Err
}
