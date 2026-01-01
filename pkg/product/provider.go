package product

import "context"

// Provider defines the interface for all product providers
type Provider interface {
	// GetType returns the product type this provider handles
	GetType() ProductType

	// GenerateConfig creates a new configuration for a subscription (PROVISIONING + GENERATION)
	GenerateConfig(ctx context.Context, sub *SubscriptionInfo, planName string) (*ConfigResult, error)

	// GenerateClientConfig generates ONLY the client configuration string based on current subscription info
	// This is used for dynamic subscription links where we want fresh links without re-provisioning the server
	GenerateClientConfig(ctx context.Context, sub *SubscriptionInfo) (string, error)

	// ActivateUser adds the user to the active service
	ActivateUser(ctx context.Context, sub *SubscriptionInfo) error

	// DeactivateUser removes the user from the active service
	DeactivateUser(ctx context.Context, sub *SubscriptionInfo) error

	// GetUsageStats retrieves current usage statistics
	GetUsageStats(ctx context.Context, sub *SubscriptionInfo) (*UsageStats, error)

	// ValidateConfig checks if a product-specific config is valid
	ValidateConfig(config string) error
}
