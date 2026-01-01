package product

import (
	"context"
	"testing"
)

// stubProvider is a minimal Provider implementation for factory tests.
// All methods return zero values — the factory only inspects GetType().
type stubProvider struct {
	t ProductType
}

func (s *stubProvider) GetType() ProductType { return s.t }
func (s *stubProvider) GenerateConfig(_ context.Context, _ *SubscriptionInfo, _ string) (*ConfigResult, error) {
	return nil, nil
}
func (s *stubProvider) GenerateClientConfig(_ context.Context, _ *SubscriptionInfo) (string, error) {
	return "", nil
}
func (s *stubProvider) ActivateUser(_ context.Context, _ *SubscriptionInfo) error   { return nil }
func (s *stubProvider) DeactivateUser(_ context.Context, _ *SubscriptionInfo) error { return nil }
func (s *stubProvider) GetUsageStats(_ context.Context, _ *SubscriptionInfo) (*UsageStats, error) {
	return nil, nil
}
func (s *stubProvider) ValidateConfig(_ string) error { return nil }

var _ Provider = (*stubProvider)(nil)

func TestProviderFactory_RegisterAndGet(t *testing.T) {
	f := NewProviderFactory()
	xray := &stubProvider{t: ProductTypeXray}
	f.Register(xray)

	got, err := f.Get(ProductTypeXray)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != xray {
		t.Error("Get should return the same provider instance")
	}
}

func TestProviderFactory_Get_Missing(t *testing.T) {
	f := NewProviderFactory()
	if _, err := f.Get(ProductTypeOpenVPN); err == nil {
		t.Fatal("unregistered type should error")
	}
}

func TestProviderFactory_GetAll_And_SupportedTypes(t *testing.T) {
	f := NewProviderFactory()
	f.Register(&stubProvider{t: ProductTypeXray})
	f.Register(&stubProvider{t: ProductTypeWireGuard})

	if got := f.GetAll(); len(got) != 2 {
		t.Errorf("GetAll() returned %d, want 2", len(got))
	}
	types := f.SupportedTypes()
	if len(types) != 2 {
		t.Errorf("SupportedTypes() returned %d, want 2", len(types))
	}
	seen := map[ProductType]bool{}
	for _, t2 := range types {
		seen[t2] = true
	}
	if !seen[ProductTypeXray] || !seen[ProductTypeWireGuard] {
		t.Errorf("missing registered types: %+v", types)
	}
}

// Re-registering the same product type overwrites the prior provider —
// supports test/restart flows that re-wire dependencies.
func TestProviderFactory_RegisterOverwrites(t *testing.T) {
	f := NewProviderFactory()
	first := &stubProvider{t: ProductTypeXray}
	second := &stubProvider{t: ProductTypeXray}
	f.Register(first)
	f.Register(second)

	got, _ := f.Get(ProductTypeXray)
	if got != second {
		t.Error("second registration should overwrite the first")
	}
}
