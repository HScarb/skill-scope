package projection_test

import (
	"testing"

	"github.com/scarb/skope/internal/projection"
)

func TestTargetNamesConflict(t *testing.T) {
	for _, tt := range []struct {
		left, right string
		want        bool
	}{
		{"Foo.md", "foo.md", true}, {"Refs", "refs", true},
		{"same", "same", true}, {"one", "two", false}, {"K", "K", true},
	} {
		if got := projection.TargetNamesConflict(tt.left, tt.right); got != tt.want {
			t.Errorf("TargetNamesConflict(%q, %q) = %v", tt.left, tt.right, got)
		}
	}
}
