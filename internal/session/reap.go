package session

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/scarb/skope/internal/skill"
)

const abandonedSessionAge = time.Hour

func (m *Manager) Reap() []error {
	root, err := openSessionsRoot(m.Home, false)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return []error{err}
	}
	entries, err := readRootDir(root)
	if err != nil {
		return []error{errors.Join(fmt.Errorf("read sessions root: %w", err), root.Close())}
	}
	var warnings []error
	for _, entry := range entries {
		name := entry.Name()
		info, err := root.Lstat(name)
		if err != nil {
			warnings = append(warnings, fmt.Errorf("inspect session %q: %w", name, err))
			continue
		}
		if isLinkOrReparse(info) {
			warnings = append(warnings, fmt.Errorf("refusing linked session child %q", name))
			continue
		}
		if !info.IsDir() {
			continue
		}
		if err := m.reapOne(root, name, info.ModTime()); err != nil {
			warnings = append(warnings, err)
		}
	}
	if err := root.Close(); err != nil {
		warnings = append(warnings, fmt.Errorf("close sessions root: %w", err))
	}
	return warnings
}

func (m *Manager) reapOne(root *os.Root, name string, modified time.Time) error {
	if strings.HasPrefix(name, ".staging-") {
		return m.removeIfOld(root, name, modified)
	}
	raw, err := readRootFile(root, filepath.Join(name, "owner.json"))
	if errors.Is(err, fs.ErrNotExist) {
		return m.removeIfOld(root, name, modified)
	}
	if err != nil {
		return fmt.Errorf("read owner for session %q: %w", name, err)
	}
	owner, err := decodeOwner(raw)
	if err != nil {
		return m.removeIfOld(root, name, modified)
	}
	token, err := m.processes().StartToken(owner.PID)
	if err != nil && !errors.Is(err, ErrProcessNotFound) {
		return fmt.Errorf("inspect owner for session %q: %w", name, err)
	}
	if errors.Is(err, ErrProcessNotFound) || token != owner.ProcessStart {
		if err := removeRootChild(root, name); err != nil {
			return fmt.Errorf("remove stale session %q: %w", name, err)
		}
	}
	return nil
}

func (m *Manager) removeIfOld(root *os.Root, name string, modified time.Time) error {
	if m.now().Sub(modified) <= abandonedSessionAge {
		return nil
	}
	if err := removeRootChild(root, name); err != nil {
		return fmt.Errorf("remove abandoned session %q: %w", name, err)
	}
	return nil
}

func readRootDir(root *os.Root) (entries []os.DirEntry, retErr error) {
	dir, err := root.Open(".")
	if err != nil {
		return nil, err
	}
	defer func() {
		if err := dir.Close(); err != nil {
			retErr = errors.Join(retErr, err)
		}
	}()
	return dir.ReadDir(-1)
}

func decodeOwner(raw []byte) (Owner, error) {
	var owner Owner
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&owner); err != nil {
		return Owner{}, err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		if err == nil {
			return Owner{}, errors.New("owner contains multiple JSON values")
		}
		return Owner{}, err
	}
	if owner.PID <= 0 || owner.ProcessStart == "" || owner.SkillSet == "" || owner.CreatedAt.IsZero() || !validOwnerAgent(owner.Agent) {
		return Owner{}, errors.New("owner is missing required fields")
	}
	return owner, nil
}

func validOwnerAgent(agent skill.Agent) bool {
	switch agent {
	case skill.AgentClaude, skill.AgentCodex, skill.AgentOpenCode:
		return true
	default:
		return false
	}
}
