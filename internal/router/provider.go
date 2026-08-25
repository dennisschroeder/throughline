package router

import (
	"context"
	"fmt"

	"github.com/dennisschroeder/throughline/internal/ports"
	"github.com/dennisschroeder/throughline/internal/registry"
)

// ProviderHandle is what a PersistenceProvider hands back for one WorkspaceTarget: a
// domain-facing Store, and an optional Close to release whatever connection or handle the
// provider opened. Close may be nil for a provider that owns no closable resource (e.g. a
// pooled connection it keeps open across targets).
type ProviderHandle struct {
	Store ports.Store
	Close func() error
}

// PersistenceProvider owns one storage topology's connection strategy and resolves a
// WorkspaceTarget to a Store for it. A provider may open one database per workspace, or
// multiplex many workspaces through one shared pool keyed by an opaque locator — the
// router and everything above it must not assume which.
type PersistenceProvider interface {
	Kind() registry.ProviderKind
	Open(ctx context.Context, target registry.WorkspaceTarget) (ProviderHandle, error)
}

// ProviderManager selects a PersistenceProvider by kind. It never interprets a target's
// ProviderLocator itself.
type ProviderManager struct {
	providers map[registry.ProviderKind]PersistenceProvider
}

func NewProviderManager(providers ...PersistenceProvider) *ProviderManager {
	manager := &ProviderManager{providers: make(map[registry.ProviderKind]PersistenceProvider, len(providers))}
	for _, provider := range providers {
		manager.providers[provider.Kind()] = provider
	}
	return manager
}

func (m *ProviderManager) Provider(kind registry.ProviderKind) (PersistenceProvider, error) {
	provider, ok := m.providers[kind]
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrProviderUnsupported, kind)
	}
	return provider, nil
}
