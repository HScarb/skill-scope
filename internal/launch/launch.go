// Package launch orchestrates session-scoped agent launches.
package launch

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"

	"github.com/scarb/skope/internal/agent"
	"github.com/scarb/skope/internal/config"
	"github.com/scarb/skope/internal/host"
	"github.com/scarb/skope/internal/session"
	"github.com/scarb/skope/internal/skill"
)

var ErrSetRequired = errors.New("skill set is required")

type AdapterRegistry interface {
	Get(skill.Agent) (agent.Adapter, bool)
}

type ExecutableResolver interface {
	LookPath(command string) (string, error)
}

type Handoff interface {
	Exec(path string, args, env []string) error
}

type SessionManager interface {
	Reap() []error
	Preview(skill.Agent, string) (*session.Session, error)
	Stage(skill.Agent, string) (*session.Session, error)
	Write(*session.Session, []session.File) error
	Publish(*session.Session) error
	Abort(*session.Session) error
}

type Request struct {
	Agent      skill.Agent
	SetValue   string
	SetPresent bool
	DryRun     bool
	AgentArgs  []string
}

type Result struct {
	Executable  string
	Args        []string
	Env         []string
	Inventory   agent.Inventory
	Resolved    skill.Resolved
	Plan        agent.LaunchPlan
	Session     *session.Session
	Warnings    []error
	NoIsolation bool
}

type Reporter func(Result) error

type Service struct {
	Env       host.Env
	FS        config.ReadFileFS
	SkopeHome string
	Registry  AdapterRegistry
	Resolver  ExecutableResolver
	Sessions  SessionManager
	Handoff   Handoff
}

func (s *Service) Run(ctx context.Context, req Request, report Reporter) error {
	if report == nil {
		return errors.New("launch reporter is required")
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	loaded, err := config.Load(s.FS, filepath.Join(s.SkopeHome, "config.toml"))
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	warnings := s.Sessions.Reap()

	if err := ctx.Err(); err != nil {
		return err
	}
	agentConfig := loaded.Agents[string(req.Agent)]
	command := agentConfig.Command
	if command == "" {
		command = string(req.Agent)
	}
	executable, err := s.Resolver.LookPath(command)
	if err != nil {
		return fmt.Errorf("[agents.%s] command %q: %w", req.Agent, command, err)
	}
	if !req.SetPresent {
		return ErrSetRequired
	}
	selected, err := config.ParseSelection(req.SetValue)
	if err != nil {
		return fmt.Errorf("parse skill set selection: %w", err)
	}

	baseArgs := joinArgs(agentConfig.Args, req.AgentArgs)
	if len(selected) == 1 && selected[0] == "none" {
		result := Result{
			Executable:  executable,
			Args:        baseArgs,
			Env:         s.Env.Environ(),
			Warnings:    append([]error(nil), warnings...),
			NoIsolation: true,
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := report(cloneResult(result)); err != nil {
			return fmt.Errorf("report launch: %w", err)
		}
		if req.DryRun {
			return nil
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := s.Handoff.Exec(executable, result.Args, result.Env); err != nil {
			return fmt.Errorf("handoff to %s: %w", req.Agent, err)
		}
		return nil
	}

	if err := ctx.Err(); err != nil {
		return err
	}
	skillSetsPath := filepath.Join(s.SkopeHome, "skillsets.toml")
	skillSets, err := config.LoadSkillSets(s.FS, skillSetsPath)
	if err != nil {
		return err
	}
	if !skillSets.Exists {
		return fmt.Errorf("%s: skill sets file does not exist", skillSetsPath)
	}
	selection, err := skillSets.Merge(selected)
	if err != nil {
		return fmt.Errorf("merge skill set selection: %w", err)
	}

	if err := ctx.Err(); err != nil {
		return err
	}
	adapter, ok := s.Registry.Get(req.Agent)
	if !ok {
		return fmt.Errorf("adapter for agent %q is not registered", req.Agent)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	inventory, err := adapter.Inventory(ctx, s.Env)
	if err != nil {
		return fmt.Errorf("inventory %s: %w", req.Agent, err)
	}
	resolved := skill.ResolveNative(req.Agent, selection.Skills, inventory.Skills)

	if err := ctx.Err(); err != nil {
		return err
	}
	var sess *session.Session
	if req.DryRun {
		sess, err = s.Sessions.Preview(req.Agent, selection.DisplayName)
	} else {
		sess, err = s.Sessions.Stage(req.Agent, selection.DisplayName)
	}
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return s.abortIfStaged(req.DryRun, sess, err)
	}
	plan, err := adapter.Plan(resolved, inventory, sess)
	if err != nil {
		return s.abortIfStaged(req.DryRun, sess, fmt.Errorf("plan %s launch: %w", req.Agent, err))
	}

	args := joinArgs(baseArgs, plan.ControlArgs)
	result := Result{
		Executable: executable,
		Args:       args,
		Env:        s.Env.With(plan.Env).Environ(),
		Inventory:  inventory,
		Resolved:   resolved,
		Plan:       plan,
		Session:    sess,
		Warnings:   append([]error(nil), warnings...),
	}
	if req.DryRun {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := report(cloneResult(result)); err != nil {
			return fmt.Errorf("report launch: %w", err)
		}
		return nil
	}

	if err := ctx.Err(); err != nil {
		return s.abort(sess, err)
	}
	if err := s.Sessions.Write(sess, sessionFiles(plan.Files)); err != nil {
		return s.abort(sess, fmt.Errorf("write session: %w", err))
	}
	if err := ctx.Err(); err != nil {
		return s.abort(sess, err)
	}
	if err := s.Sessions.Publish(sess); err != nil {
		return s.abort(sess, fmt.Errorf("publish session: %w", err))
	}
	if err := ctx.Err(); err != nil {
		return s.abort(sess, err)
	}
	if err := report(cloneResult(result)); err != nil {
		return s.abort(sess, fmt.Errorf("report launch: %w", err))
	}
	if err := ctx.Err(); err != nil {
		return s.abort(sess, err)
	}
	if err := s.Handoff.Exec(executable, result.Args, result.Env); err != nil {
		return s.abort(sess, fmt.Errorf("handoff to %s: %w", req.Agent, err))
	}
	return nil
}

func (s *Service) abortIfStaged(dryRun bool, sess *session.Session, cause error) error {
	if dryRun {
		return cause
	}
	return s.abort(sess, cause)
}

func (s *Service) abort(sess *session.Session, cause error) error {
	if err := s.Sessions.Abort(sess); err != nil {
		return errors.Join(cause, fmt.Errorf("abort session: %w", err))
	}
	return cause
}

func joinArgs(groups ...[]string) []string {
	length := 0
	for _, group := range groups {
		length += len(group)
	}
	joined := make([]string, 0, length)
	for _, group := range groups {
		joined = append(joined, group...)
	}
	return joined
}

func sessionFiles(planned []agent.PlannedFile) []session.File {
	files := make([]session.File, len(planned))
	for i, file := range planned {
		files[i] = session.File{
			Path: file.Path,
			Data: append([]byte(nil), file.Data...),
			Mode: file.Mode,
		}
	}
	return files
}

func cloneResult(source Result) Result {
	cloned := source
	cloned.Args = append([]string(nil), source.Args...)
	cloned.Env = append([]string(nil), source.Env...)
	cloned.Inventory = cloneInventory(source.Inventory)
	cloned.Resolved = cloneResolved(source.Resolved)
	cloned.Plan = clonePlan(source.Plan)
	cloned.Warnings = append([]error(nil), source.Warnings...)
	if source.Session != nil {
		cloned.Session = &session.Session{Root: source.Session.Root, Agent: source.Session.Agent}
	}
	return cloned
}

func cloneInventory(source agent.Inventory) agent.Inventory {
	cloned := agent.Inventory{
		Skills:     make([]skill.Skill, len(source.Skills)),
		SkillNames: append([]string(nil), source.SkillNames...),
		PluginIDs:  append([]string(nil), source.PluginIDs...),
		Collisions: make([]skill.Collision, len(source.Collisions)),
		Warnings:   append([]string(nil), source.Warnings...),
	}
	for i, candidate := range source.Skills {
		cloned.Skills[i] = candidate
		cloned.Skills[i].Locations = make([]skill.Location, len(candidate.Locations))
		for j, location := range candidate.Locations {
			cloned.Skills[i].Locations[j] = cloneLocation(location)
		}
	}
	for i, collision := range source.Collisions {
		cloned.Collisions[i] = collision
		cloned.Collisions[i].IDs = append([]string(nil), collision.IDs...)
		cloned.Collisions[i].Paths = append([]string(nil), collision.Paths...)
	}
	return cloned
}

func cloneResolved(source skill.Resolved) skill.Resolved {
	cloned := skill.Resolved{Agent: source.Agent, Entries: make([]skill.Resolution, len(source.Entries))}
	for i, resolution := range source.Entries {
		cloned.Entries[i] = resolution
		cloned.Entries[i].Names = append([]string(nil), resolution.Names...)
		if resolution.Location != nil {
			location := cloneLocation(*resolution.Location)
			cloned.Entries[i].Location = &location
		}
	}
	return cloned
}

func cloneLocation(source skill.Location) skill.Location {
	cloned := source
	cloned.Names = make(map[skill.Agent]string, len(source.Names))
	for name, value := range source.Names {
		cloned.Names[name] = value
	}
	return cloned
}

func clonePlan(source agent.LaunchPlan) agent.LaunchPlan {
	cloned := agent.LaunchPlan{
		ControlArgs: append([]string(nil), source.ControlArgs...),
		Env:         make(map[string]string, len(source.Env)),
		Files:       make([]agent.PlannedFile, len(source.Files)),
	}
	for name, value := range source.Env {
		cloned.Env[name] = value
	}
	for i, file := range source.Files {
		cloned.Files[i] = file
		cloned.Files[i].Data = append([]byte(nil), file.Data...)
	}
	return cloned
}
