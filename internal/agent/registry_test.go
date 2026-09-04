package agent_test

import (
	"context"
	"strings"
	"testing"

	"github.com/scarb/skope/internal/agent"
	"github.com/scarb/skope/internal/host"
	"github.com/scarb/skope/internal/session"
	"github.com/scarb/skope/internal/skill"
)

func TestRegistryFindsRegisteredAdapters(t *testing.T) {
	t.Parallel()

	claude := fakeAdapter{name: skill.AgentClaude}
	codex := fakeAdapter{name: skill.AgentCodex}
	registry, err := agent.NewRegistry(claude, codex)
	if err != nil {
		t.Fatalf("NewRegistry() error = %v", err)
	}

	got, ok := registry.Get(skill.AgentClaude)
	if !ok || got.Name() != skill.AgentClaude {
		t.Fatalf("Get(claude) = %v, %v", got, ok)
	}
	if _, ok := registry.Get(skill.AgentOpenCode); ok {
		t.Fatal("Get(opencode) found an unregistered adapter")
	}
}

func TestRegistryRejectsDuplicateNames(t *testing.T) {
	t.Parallel()

	_, err := agent.NewRegistry(
		fakeAdapter{name: skill.AgentClaude},
		fakeAdapter{name: skill.AgentClaude},
	)
	if err == nil || !strings.Contains(err.Error(), string(skill.AgentClaude)) {
		t.Fatalf("NewRegistry() error = %v, want duplicate name", err)
	}
}

func TestRegistryRejectsNilAdapter(t *testing.T) {
	t.Parallel()

	_, err := agent.NewRegistry(nil)
	if err == nil || !strings.Contains(err.Error(), "nil adapter") {
		t.Fatalf("NewRegistry() error = %v, want nil adapter", err)
	}
}

type fakeAdapter struct {
	name skill.Agent
}

func (f fakeAdapter) Name() skill.Agent {
	return f.name
}

func (fakeAdapter) Capabilities() agent.Capabilities {
	return agent.Capabilities{}
}

func (fakeAdapter) Inventory(context.Context, host.Env) (agent.Inventory, error) {
	return agent.Inventory{}, nil
}

func (fakeAdapter) Plan(skill.Resolved, agent.Inventory, *session.Session) (agent.LaunchPlan, error) {
	return agent.LaunchPlan{}, nil
}
