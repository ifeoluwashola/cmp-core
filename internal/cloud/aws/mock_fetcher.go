// internal/cloud/aws/mock_fetcher.go
// Mock AWS cloud provider for local development and testing.
//
// Replace this with a real implementation that calls the AWS SDK once you
// have IAM cross-account roles configured. The interface contract is
// identical — only FetchResources changes.

package aws

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/ifeoluwashola/cmp-core/internal/models"
)

// MockFetcher implements cloud.Provider with deterministic dummy data.
// It returns two resources per environment: an EC2 instance and an EKS cluster.
type MockFetcher struct{}

// NewMockFetcher constructs a MockFetcher.
func NewMockFetcher() *MockFetcher { return &MockFetcher{} }

// FetchResources returns two hard-coded AWS resources with realistic JSONB attributes.
func (m *MockFetcher) FetchResources(ctx context.Context, env models.CloudEnvironment) ([]models.InfrastructureResource, error) {
	now := time.Now().UTC()

	ec2Attrs, err := json.Marshal(map[string]any{
		"region":        "us-east-1",
		"instance_type": "t3.medium",
		"state":         "running",
		"tags": map[string]string{
			"Name":        "web-server-01",
			"Environment": env.Name,
		},
		"private_ip": "10.0.1.42",
	})
	if err != nil {
		return nil, fmt.Errorf("aws mock: marshal ec2 attrs: %w", err)
	}

	eksAttrs, err := json.Marshal(map[string]any{
		"region":          "us-east-1",
		"cluster_version": "1.29",
		"status":          "ACTIVE",
		"node_count":      3,
		"tags": map[string]string{
			"Name":        "prod-cluster",
			"Environment": env.Name,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("aws mock: marshal eks attrs: %w", err)
	}

	resources := []models.InfrastructureResource{
		{
			// ID will be set by the database on insert; use a deterministic
			// provider-side ID so ON CONFLICT deduplicates correctly.
			ID:                 uuid.Nil, // populated after DB upsert
			OrganizationID:     env.OrganizationID,
			EnvironmentID:      env.ID,
			ProviderResourceID: fmt.Sprintf("i-mock-%s", env.ID.String()[:8]),
			ResourceType:       "aws:ec2:instance",
			Attributes:         json.RawMessage(ec2Attrs),
			Status:             "running",
			LastAuditedAt:      &now,
		},
		{
			ID:                 uuid.Nil,
			OrganizationID:     env.OrganizationID,
			EnvironmentID:      env.ID,
			ProviderResourceID: fmt.Sprintf("eks-mock-%s", env.ID.String()[:8]),
			ResourceType:       "aws:eks:cluster",
			Attributes:         json.RawMessage(eksAttrs),
			Status:             "active",
			LastAuditedAt:      &now,
		},
	}

	return resources, nil
}
