// internal/cloud/provider.go
// Cloud provider abstraction layer.
//
// Each cloud integration (AWS, GCP, Azure…) must implement the Provider
// interface. The Registry maps a models.CloudProvider enum value to the
// concrete implementation so the auditor can dispatch dynamically.

package cloud

import (
	"context"
	"fmt"

	"github.com/ifeoluwashola/cmp-core/internal/models"
)

// Provider is the contract every cloud integration must fulfil.
// FetchResources discovers the infrastructure resources for a single cloud
// environment and returns them as model structs ready for upsert.
// FetchCosts retrieves daily billing records for the environment.
type Provider interface {
	FetchResources(ctx context.Context, env models.CloudEnvironment) ([]models.InfrastructureResource, error)
	FetchCosts(ctx context.Context, env models.CloudEnvironment) ([]models.DailyCost, error)
}

// Registry maps a CloudProvider enum to its live implementation.
// Populate this at startup in main.go and inject it into the Auditor.
type Registry map[models.CloudProvider]Provider

// Get returns the Provider for the given cloud, or an error if unsupported.
func (r Registry) Get(p models.CloudProvider) (Provider, error) {
	impl, ok := r[p]
	if !ok {
		return nil, fmt.Errorf("cloud: no provider registered for %q", p)
	}
	return impl, nil
}
