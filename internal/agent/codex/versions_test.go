package codex_test

import (
	"testing"

	"github.com/scarb/skope/internal/agent/codex"
)

func TestVersionsTotalOrder(t *testing.T) {
	for _, pair := range [][2]string{{"10.0.0", "9.0.0"}, {"aaa", "10.0.0"}, {"local", "zzz"}, {"1.0.0-alpha.10", "1.0.0-alpha.2"}, {"1.0.0", "1.0.0-rc"}, {"1.0.0+10", "1.0.0+2"}, {"1.0.0+2", "1.0.0"}, {"1.0.0+00", "1.0.0+0"}, {"1.0.0+01", "1.0.0+1"}, {"1.0.0+001", "1.0.0+01"}, {"1.0.0+2", "1.0.0+001"}, {"1.0.0+a", "1.0.0+999"}, {"1.0.0+a.b", "1.0.0+a"}, {"1.0.0-99999999999999999999999999", "1.0.0-9999999999999999999999999"}, {"9.0.0", "18446744073709551616.0.0"}, {"9.0.0", "01.0.0"}, {"1.0.0-2", "1.0.0-01"}, {"1.0.0+x", "1.0.0+"}} {
		t.Run(pair[0]+"_"+pair[1], func(t *testing.T) {
			if codex.CompareVersionsForTest(pair[0], pair[1]) <= 0 || codex.CompareVersionsForTest(pair[1], pair[0]) >= 0 {
				t.Fatal(pair)
			}
		})
	}
}
func TestVersionsRequireDominantCandidate(t *testing.T) {
	cycle := []string{"9.0.0", "10.0.0", "2x"}
	if _, err := codex.ActiveVersionForTest(cycle); err == nil {
		t.Fatal("cycle accepted")
	}
	for _, winner := range []string{"aaa", "local"} {
		got, err := codex.ActiveVersionForTest(append(cycle, winner))
		if err != nil || got != winner {
			t.Fatalf("%s %v", got, err)
		}
	}
}
