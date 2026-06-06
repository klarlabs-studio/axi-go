// Package inmemory provides in-memory implementations of all repository ports.
package inmemory

import (
	"sync"

	"go.klarlabs.de/axi/domain"
)

// Compile-time interface satisfaction checks.
var (
	_ domain.ActionRepository     = (*ActionDefinitionRepository)(nil)
	_ domain.CapabilityRepository = (*CapabilityDefinitionRepository)(nil)
	_ domain.PluginRepository     = (*PluginContributionRepository)(nil)
	_ domain.SessionRepository    = (*ExecutionSessionRepository)(nil)
)

// ActionDefinitionRepository is an in-memory action repository.
type ActionDefinitionRepository struct {
	mu      sync.RWMutex
	actions map[domain.ActionName]*domain.ActionDefinition
}

func NewActionDefinitionRepository() *ActionDefinitionRepository {
	return &ActionDefinitionRepository{
		actions: make(map[domain.ActionName]*domain.ActionDefinition),
	}
}

func (r *ActionDefinitionRepository) GetByName(name domain.ActionName) (*domain.ActionDefinition, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	a, ok := r.actions[name]
	if !ok {
		return nil, &domain.ErrNotFound{Entity: "action", ID: string(name)}
	}
	return a, nil
}

func (r *ActionDefinitionRepository) Save(action *domain.ActionDefinition) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.actions[action.Name()] = action
	return nil
}

func (r *ActionDefinitionRepository) Delete(name domain.ActionName) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.actions, name)
	return nil
}

func (r *ActionDefinitionRepository) List() []*domain.ActionDefinition {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make([]*domain.ActionDefinition, 0, len(r.actions))
	for _, a := range r.actions {
		result = append(result, a)
	}
	return result
}

// CapabilityDefinitionRepository is an in-memory capability repository.
type CapabilityDefinitionRepository struct {
	mu           sync.RWMutex
	capabilities map[domain.CapabilityName]*domain.CapabilityDefinition
}

func NewCapabilityDefinitionRepository() *CapabilityDefinitionRepository {
	return &CapabilityDefinitionRepository{
		capabilities: make(map[domain.CapabilityName]*domain.CapabilityDefinition),
	}
}

func (r *CapabilityDefinitionRepository) GetByName(name domain.CapabilityName) (*domain.CapabilityDefinition, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	c, ok := r.capabilities[name]
	if !ok {
		return nil, &domain.ErrNotFound{Entity: "capability", ID: string(name)}
	}
	return c, nil
}

func (r *CapabilityDefinitionRepository) Save(capability *domain.CapabilityDefinition) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.capabilities[capability.Name()] = capability
	return nil
}

func (r *CapabilityDefinitionRepository) Delete(name domain.CapabilityName) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.capabilities, name)
	return nil
}

func (r *CapabilityDefinitionRepository) List() []*domain.CapabilityDefinition {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make([]*domain.CapabilityDefinition, 0, len(r.capabilities))
	for _, c := range r.capabilities {
		result = append(result, c)
	}
	return result
}

// PluginContributionRepository is an in-memory plugin repository.
type PluginContributionRepository struct {
	mu      sync.RWMutex
	plugins map[domain.PluginID]*domain.PluginContribution
}

func NewPluginContributionRepository() *PluginContributionRepository {
	return &PluginContributionRepository{
		plugins: make(map[domain.PluginID]*domain.PluginContribution),
	}
}

func (r *PluginContributionRepository) Save(contribution *domain.PluginContribution) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.plugins[contribution.PluginID()] = contribution
	return nil
}

func (r *PluginContributionRepository) GetByID(id domain.PluginID) (*domain.PluginContribution, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	p, ok := r.plugins[id]
	if !ok {
		return nil, &domain.ErrNotFound{Entity: "plugin", ID: string(id)}
	}
	return p, nil
}

func (r *PluginContributionRepository) Delete(id domain.PluginID) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.plugins, id)
	return nil
}

func (r *PluginContributionRepository) Exists(id domain.PluginID) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, ok := r.plugins[id]
	return ok
}

// ExecutionSessionRepository is an in-memory session repository.
type ExecutionSessionRepository struct {
	mu       sync.RWMutex
	sessions map[domain.ExecutionSessionID]*domain.ExecutionSession
}

func NewExecutionSessionRepository() *ExecutionSessionRepository {
	return &ExecutionSessionRepository{
		sessions: make(map[domain.ExecutionSessionID]*domain.ExecutionSession),
	}
}

func (r *ExecutionSessionRepository) Save(session *domain.ExecutionSession) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sessions[session.ID()] = session
	return nil
}

func (r *ExecutionSessionRepository) Get(id domain.ExecutionSessionID) (*domain.ExecutionSession, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	s, ok := r.sessions[id]
	if !ok {
		return nil, &domain.ErrNotFound{Entity: "session", ID: string(id)}
	}
	return s, nil
}
