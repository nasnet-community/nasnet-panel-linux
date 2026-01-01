package product

import (
	"fmt"
	"sync"
)

// ProviderFactory manages and creates product providers
type ProviderFactory struct {
	providers map[ProductType]Provider
	mu        sync.RWMutex
}

// NewProviderFactory creates a new provider factory
func NewProviderFactory() *ProviderFactory {
	return &ProviderFactory{
		providers: make(map[ProductType]Provider),
	}
}

// Register adds a provider for a product type
func (f *ProviderFactory) Register(provider Provider) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.providers[provider.GetType()] = provider
}

// Get returns the provider for a product type
func (f *ProviderFactory) Get(productType ProductType) (Provider, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()

	provider, ok := f.providers[productType]
	if !ok {
		return nil, fmt.Errorf("no provider registered for product type: %s", productType)
	}
	return provider, nil
}

// GetAll returns all registered providers
func (f *ProviderFactory) GetAll() []Provider {
	f.mu.RLock()
	defer f.mu.RUnlock()

	providers := make([]Provider, 0, len(f.providers))
	for _, p := range f.providers {
		providers = append(providers, p)
	}
	return providers
}

// SupportedTypes returns all registered product types
func (f *ProviderFactory) SupportedTypes() []ProductType {
	f.mu.RLock()
	defer f.mu.RUnlock()

	types := make([]ProductType, 0, len(f.providers))
	for t := range f.providers {
		types = append(types, t)
	}
	return types
}
