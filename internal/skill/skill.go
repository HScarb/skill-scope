package skill

type Agent string
type Kind string
type Level string
type Source string

const (
	AgentClaude   Agent = "claude"
	AgentCodex    Agent = "codex"
	AgentOpenCode Agent = "opencode"

	KindSkill   Kind = "skill"
	KindCommand Kind = "command"

	LevelGlobal  Level = "global"
	LevelProject Level = "project"
	LevelPlugin  Level = "plugin"
	LevelAdmin   Level = "admin"

	SourceClaude       Source = "claude"
	SourceAgents       Source = "agents"
	SourceCodex        Source = "codex"
	SourceOpenCode     Source = "opencode"
	SourceOpenCodePath Source = "opencode-paths"
)

type Skill struct {
	ID        string
	Locations []Location
}

type Location struct {
	Kind            Kind
	DiscoveryPath   string
	RealPath        string
	Level           Level
	Source          Source
	Scope           string
	FrontmatterName string
	Names           map[Agent]string
	PluginID        string
	PluginAgent     Agent
}

type CollisionKind string

const (
	CollisionDifferentContent CollisionKind = "different-content"
	CollisionFrontmatterName  CollisionKind = "frontmatter-name"
	CollisionEffectiveName    CollisionKind = "effective-name"
)

type Collision struct {
	Kind  CollisionKind
	IDs   []string
	Agent Agent
	Name  string
	Paths []string
}
