package session_test

import (
	"path/filepath"
	"reflect"
	"testing"

	"github.com/scarb/skope/internal/session"
	"github.com/scarb/skope/internal/skill"
)

func TestSessionAgentPath(t *testing.T) {
	t.Parallel()

	finalRoot := filepath.Join("final", "sessions", "abc")
	sess := session.Session{Root: finalRoot, Agent: skill.AgentClaude}
	parts := []string{"settings.json"}
	wantParts := append([]string(nil), parts...)

	got := sess.AgentPath(parts...)
	want := filepath.Join(finalRoot, "claude", "settings.json")
	if got != want {
		t.Fatalf("AgentPath() = %q, want %q", got, want)
	}
	if !reflect.DeepEqual(parts, wantParts) {
		t.Fatalf("AgentPath() changed parts to %v, want %v", parts, wantParts)
	}
}
