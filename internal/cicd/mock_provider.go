// internal/cicd/mock_provider.go
// Mock CI/CD provider for local development and testing.
//
// Replace this with a real implementation (e.g. GitHub Actions REST API,
// GitLab pipeline trigger) once a CI/CD system is configured. The interface
// contract is identical — only TriggerDeployment changes.

package cicd

import (
	"context"
	"log"

	"github.com/google/uuid"
	"github.com/ifeoluwashola/cmp-core/internal/models"
)

// MockProvider implements PipelineProvider with deterministic dummy behaviour.
// It logs the trigger and returns a random UUID as the job ID.
type MockProvider struct{}

// NewMockProvider constructs a MockProvider.
func NewMockProvider() *MockProvider { return &MockProvider{} }

// TriggerDeployment simulates kicking off an IaC pipeline run.
// In production this would call a CI/CD API (e.g. POST to GitHub Actions workflow).
func (m *MockProvider) TriggerDeployment(ctx context.Context, deployment models.Deployment) (string, error) {
	jobID := uuid.New().String()
	log.Printf(
		"cicd mock: triggered deployment id=%s env=%s module=%s → job_id=%s",
		deployment.ID, deployment.EnvironmentID, deployment.ModuleName, jobID,
	)
	return jobID, nil
}
