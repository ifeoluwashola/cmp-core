# Project Specification: Multi-Tenant Cloud Management & FinOps Platform (MVP)

## 1. Executive Summary

This document outlines the architecture and data model for a multi-tenant Cloud Management Platform (CMP). The platform is designed specifically for consulting and MSP models, providing business stakeholders with clear, readable dashboards for infrastructure state and cost visibility, while providing the engineering team with a scalable, secure control plane for cross-account management.

## 2. Core Architecture: The Hub and Spoke Model

The system utilizes a Control Plane (Hub) and Data Plane (Spoke) architecture to ensure strict isolation, scalability, and zero-trust security across multiple client organizations.

### 2.1 The Control Plane (The Hub)

Hosted and managed centrally, this cluster handles state, multi-tenant authentication, and orchestrates commands.

* **Ingress & Routing:** Traffic is managed via a Kubernetes Gateway API implementation (e.g., Istio) to handle TLS termination, rate-limiting per client, and zero-trust routing between microservices.
* **Core Microservices (Go Backend):**
  * **Identity Service:** Manages RBAC and organization mapping.
  * **Auditing Service:** A highly concurrent backend that polls target cloud environments in parallel to fetch infrastructure states.
  * **FinOps Engine:** Ingests and normalizes billing data from cloud provider APIs.
  * **Provisioning Orchestrator:** Dispatches deployment payloads to the execution environment.
* **Observability Stack:** Prometheus, Grafana, Loki, and Tempo for internal metrics, log aggregation, and distributed tracing.

### 2.2 The Execution Environment (CI/CD Bridge)

Infrastructure as Code (IaC) execution is abstracted from the core backend to ensure stability.

* The Provisioning Orchestrator triggers deployment pipelines (via GitHub Actions, GitLab CI, etc.).
* Pipelines execute standardized Terraform/Ansible modules against client environments.
* Webhooks return execution state (success/failure) back to the Control Plane.

### 2.3 Client Environments (The Spokes)

Client cloud accounts (AWS, GCP, Azure) are integrated without storing long-lived static credentials.

* **Authentication:** Utilizes OpenID Connect (OIDC) or Cross-Account IAM Roles (STS).
* **Access Level (MVP):** Read-only roles provisioned via a one-time onboarding script to allow the Auditing and FinOps services to scan resources and billing data safely.

---

## 3. Database Schema (PostgreSQL)

The database leverages logical separation with Row-Level Security (RLS) to guarantee strict data isolation between client organizations. `JSONB` is utilized for flexible storage of unpredictable cloud resource attributes.

### 3.1 Identity & Access

* **`organizations`**: `id` (UUID, PK), `name` (String), `tier` (String), `created_at` (Timestamp)
* **`users`**: `id` (UUID, PK), `organization_id` (UUID, FK), `email` (String), `role` (Enum), `password_hash` (String)

### 3.2 Spoke Connections (Cloud Accounts)

* **`cloud_environments`**: `id` (UUID, PK), `organization_id` (UUID, FK), `name` (String), `provider` (Enum), `auth_type` (Enum), `role_arn` (String), `connection_status` (Enum)

### 3.3 State & Discovery

* **`infrastructure_resources`**: `id` (UUID, PK), `organization_id` (UUID, FK), `environment_id` (UUID, FK), `provider_resource_id` (String), `resource_type` (String), `attributes` (JSONB), `status` (String), `last_audited_at` (Timestamp)

### 3.4 FinOps (Cost Tracking)

* **`daily_costs`**: `id` (UUID, PK), `organization_id` (UUID, FK), `environment_id` (UUID, FK), `date` (Date), `service_category` (String), `amount` (Decimal), `currency` (String)

---

## 4. Phased Implementation Plan

### Phase 1: The Auditor (MVP)

* **Goal:** Read-only visibility and multi-tenant foundation.
* **Deliverables:** Organization and user management, OIDC role integration, concurrent infrastructure polling, cost ingestion, and read-only client dashboards.

### Phase 2: The Provisioner

* **Goal:** Write access and automated deployments.
* **Deliverables:** Webhook listener, CI/CD pipeline triggers, Terraform module execution, and real-time deployment status tracking.
