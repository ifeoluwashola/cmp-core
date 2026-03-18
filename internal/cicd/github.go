package cicd

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"

	"github.com/google/uuid"
)

// GitHubClient handles triggering real CI/CD deployments natively via GitHub Actions workflow_dispatch.
type GitHubClient struct {
	Token string
	Owner string
	Repo  string
}

// NewGitHubClient securely extracts standard system environment configuration targeting the primary Action Dispatch.
func NewGitHubClient() (*GitHubClient, error) {
	token := os.Getenv("GITHUB_TOKEN")
	if token == "" {
		return nil, fmt.Errorf("GITHUB_TOKEN is required to initialize the CI/CD integration")
	}

	owner := os.Getenv("GITHUB_OWNER")
	if owner == "" {
		return nil, fmt.Errorf("GITHUB_OWNER is required to initialize the CI/CD integration")
	}

	repo := os.Getenv("GITHUB_REPO")
	if repo == "" {
		return nil, fmt.Errorf("GITHUB_REPO is required to initialize the CI/CD integration")
	}

	return &GitHubClient{
		Token: token,
		Owner: owner,
		Repo:  repo,
	}, nil
}

// workflowDispatchPayload mirrors the precise JSON schema utilized by GitHub's raw pipeline dispatches.
type workflowDispatchPayload struct {
	Ref    string            `json:"ref"`
	Inputs map[string]string `json:"inputs"`
}

// TriggerWorkflow initiates an execution instance of `deploy.yml`.
func (c *GitHubClient) TriggerWorkflow(ctx context.Context, deploymentID uuid.UUID, moduleName string, envID uuid.UUID, provisioningRoleARN string) error {
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/actions/workflows/deploy.yml/dispatches", c.Owner, c.Repo)

	payload := workflowDispatchPayload{
		Ref: "master",
		Inputs: map[string]string{
			"deployment_id":       deploymentID.String(),
			"module_name":         moduleName,
			"environment_id":      envID.String(),
			"provisioning_role_arn": provisioningRoleARN,
		},
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal github payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(body))
	if err != nil {
		return fmt.Errorf("failed forming github http request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+c.Token)
	req.Header.Set("Accept", "application/vnd.github.v3+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("trigger workflow execution failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("github api returned unexpected status: %s", resp.Status)
	}

	return nil
}
