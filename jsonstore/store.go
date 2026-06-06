// Package jsonstore provides file-based JSON persistence adapters
// for all axi-go repository interfaces. Zero external dependencies.
//
// Data is stored as JSON files in a directory:
//
//	data/
//	  actions/{name}.json
//	  capabilities/{name}.json
//	  plugins/{id}.json
//	  sessions/{id}.json
package jsonstore

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"

	"go.klarlabs.de/axi/domain"
)

// Compile-time interface checks.
var (
	_ domain.ActionRepository     = (*ActionStore)(nil)
	_ domain.CapabilityRepository = (*CapabilityStore)(nil)
	_ domain.PluginRepository     = (*PluginStore)(nil)
	_ domain.SessionRepository    = (*SessionStore)(nil)
)

// --- ActionStore ---

// ActionStore persists ActionDefinitions as JSON files.
type ActionStore struct {
	mu  sync.RWMutex
	dir string
}

// NewActionStore creates an ActionStore writing to dir/actions/.
func NewActionStore(dir string) (*ActionStore, error) {
	d := filepath.Join(dir, "actions")
	if err := os.MkdirAll(d, 0o755); err != nil {
		return nil, err
	}
	return &ActionStore{dir: d}, nil
}

func (s *ActionStore) Save(action *domain.ActionDefinition) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return writeJSON(filepath.Join(s.dir, string(action.Name())+".json"), action.ToSnapshot())
}

func (s *ActionStore) GetByName(name domain.ActionName) (*domain.ActionDefinition, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var snap domain.ActionSnapshot
	if err := readJSON(filepath.Join(s.dir, string(name)+".json"), &snap); err != nil {
		return nil, &domain.ErrNotFound{Entity: "action", ID: string(name)}
	}
	return domain.ActionFromSnapshot(snap)
}

func (s *ActionStore) Delete(name domain.ActionName) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return os.Remove(filepath.Join(s.dir, string(name)+".json"))
}

func (s *ActionStore) List() []*domain.ActionDefinition {
	s.mu.RLock()
	defer s.mu.RUnlock()
	entries, _ := os.ReadDir(s.dir)
	var result []*domain.ActionDefinition
	for _, e := range entries {
		var snap domain.ActionSnapshot
		if err := readJSON(filepath.Join(s.dir, e.Name()), &snap); err == nil {
			if a, err := domain.ActionFromSnapshot(snap); err == nil {
				result = append(result, a)
			}
		}
	}
	return result
}

// --- CapabilityStore ---

// CapabilityStore persists CapabilityDefinitions as JSON files.
type CapabilityStore struct {
	mu  sync.RWMutex
	dir string
}

// NewCapabilityStore creates a CapabilityStore writing to dir/capabilities/.
func NewCapabilityStore(dir string) (*CapabilityStore, error) {
	d := filepath.Join(dir, "capabilities")
	if err := os.MkdirAll(d, 0o755); err != nil {
		return nil, err
	}
	return &CapabilityStore{dir: d}, nil
}

func (s *CapabilityStore) Save(c *domain.CapabilityDefinition) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return writeJSON(filepath.Join(s.dir, string(c.Name())+".json"), c.ToSnapshot())
}

func (s *CapabilityStore) GetByName(name domain.CapabilityName) (*domain.CapabilityDefinition, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var snap domain.CapabilitySnapshot
	if err := readJSON(filepath.Join(s.dir, string(name)+".json"), &snap); err != nil {
		return nil, &domain.ErrNotFound{Entity: "capability", ID: string(name)}
	}
	return domain.CapabilityFromSnapshot(snap)
}

func (s *CapabilityStore) Delete(name domain.CapabilityName) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return os.Remove(filepath.Join(s.dir, string(name)+".json"))
}

func (s *CapabilityStore) List() []*domain.CapabilityDefinition {
	s.mu.RLock()
	defer s.mu.RUnlock()
	entries, _ := os.ReadDir(s.dir)
	var result []*domain.CapabilityDefinition
	for _, e := range entries {
		var snap domain.CapabilitySnapshot
		if err := readJSON(filepath.Join(s.dir, e.Name()), &snap); err == nil {
			if c, err := domain.CapabilityFromSnapshot(snap); err == nil {
				result = append(result, c)
			}
		}
	}
	return result
}

// --- PluginStore ---

// PluginStore persists PluginContributions as JSON files.
type PluginStore struct {
	mu  sync.RWMutex
	dir string
}

// NewPluginStore creates a PluginStore writing to dir/plugins/.
func NewPluginStore(dir string) (*PluginStore, error) {
	d := filepath.Join(dir, "plugins")
	if err := os.MkdirAll(d, 0o755); err != nil {
		return nil, err
	}
	return &PluginStore{dir: d}, nil
}

func (s *PluginStore) Save(p *domain.PluginContribution) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return writeJSON(filepath.Join(s.dir, string(p.PluginID())+".json"), p.ToSnapshot())
}

func (s *PluginStore) GetByID(id domain.PluginID) (*domain.PluginContribution, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var snap domain.PluginSnapshot
	if err := readJSON(filepath.Join(s.dir, string(id)+".json"), &snap); err != nil {
		return nil, &domain.ErrNotFound{Entity: "plugin", ID: string(id)}
	}
	// Reconstruct — note: plugin snapshot is metadata only, not full aggregate state.
	// For the Exists() check this is sufficient.
	return reconstructPlugin(snap)
}

func (s *PluginStore) Delete(id domain.PluginID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return os.Remove(filepath.Join(s.dir, string(id)+".json"))
}

func (s *PluginStore) Exists(id domain.PluginID) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, err := os.Stat(filepath.Join(s.dir, string(id)+".json"))
	return err == nil
}

func reconstructPlugin(snap domain.PluginSnapshot) (*domain.PluginContribution, error) {
	actions := make([]*domain.ActionDefinition, len(snap.Actions))
	for i, a := range snap.Actions {
		action, err := domain.ActionFromSnapshot(a)
		if err != nil {
			return nil, err
		}
		actions[i] = action
	}
	caps := make([]*domain.CapabilityDefinition, len(snap.Capabilities))
	for i, snapCap := range snap.Capabilities {
		capDef, err := domain.CapabilityFromSnapshot(snapCap)
		if err != nil {
			return nil, err
		}
		caps[i] = capDef
	}
	id, err := domain.NewPluginID(snap.PluginID)
	if err != nil {
		return nil, err
	}
	return domain.NewPluginContribution(id, actions, caps)
}

// --- SessionStore ---

// SessionStore persists ExecutionSessions as JSON files.
type SessionStore struct {
	mu  sync.RWMutex
	dir string
}

// NewSessionStore creates a SessionStore writing to dir/sessions/.
func NewSessionStore(dir string) (*SessionStore, error) {
	d := filepath.Join(dir, "sessions")
	if err := os.MkdirAll(d, 0o755); err != nil {
		return nil, err
	}
	return &SessionStore{dir: d}, nil
}

func (s *SessionStore) Save(session *domain.ExecutionSession) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return writeJSON(filepath.Join(s.dir, string(session.ID())+".json"), session.ToSnapshot())
}

func (s *SessionStore) Get(id domain.ExecutionSessionID) (*domain.ExecutionSession, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var snap domain.SessionSnapshot
	if err := readJSON(filepath.Join(s.dir, string(id)+".json"), &snap); err != nil {
		return nil, &domain.ErrNotFound{Entity: "session", ID: string(id)}
	}
	return domain.SessionFromSnapshot(snap)
}

// --- helpers ---

func writeJSON(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func readJSON(path string, v any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, v)
}
