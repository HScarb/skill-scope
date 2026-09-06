package codex

import "fmt"

// InventoryError identifies a failed inventory source without exposing its contents.
type InventoryError struct {
	Path  string
	Field string
	Cause error
}

func (e *InventoryError) Error() string {
	return fmt.Sprintf("Codex inventory: %s (%s)", e.Field, e.Path)
}
func (e *InventoryError) Unwrap() error { return e.Cause }
func inventoryError(path, field string, cause error) error {
	return &InventoryError{Path: path, Field: field, Cause: cause}
}
