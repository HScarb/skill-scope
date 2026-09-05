package agent

import (
	"fmt"

	"github.com/scarb/skope/internal/skill"
)

type Registry struct {
	adapters map[skill.Agent]Adapter
}

func NewRegistry(adapters ...Adapter) (Registry, error) {
	registered := make(map[skill.Agent]Adapter, len(adapters))
	for _, adapter := range adapters {
		if adapter == nil {
			return Registry{}, fmt.Errorf("register nil adapter")
		}
		name := adapter.Name()
		if _, exists := registered[name]; exists {
			return Registry{}, fmt.Errorf("register duplicate adapter %q", name)
		}
		registered[name] = adapter
	}
	return Registry{adapters: registered}, nil
}

func (r Registry) Get(name skill.Agent) (Adapter, bool) {
	adapter, ok := r.adapters[name]
	return adapter, ok
}
