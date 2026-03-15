// internal/cicd/provider.go
// CI/CD pipeline abstraction layer for the Provisioning Engine.
//
// Every CI/CD backend (GitHub Actions, GitLab CI, Jenkins, …) must implement
// PipelineProvider. The concrete implementation is injected at startup so the
// handler layer stays decoupled from any specific vendor.

package cicd

import (
	"context"

	"github.com/ifeoluwashola/cmp-core/internal/models"
)

// PipelineProvider is the contract every CI/CD integration must fulfil.
// TriggerDeployment kicks off an IaC pipeline run for the given deployment and
// returns an opaque job ID that can be used to correlate incoming webhook callbacks.
type PipelineProvider interface {
	TriggerDeployment(ctx context.Context, deployment models.Deployment) (jobID string, err error)
}
