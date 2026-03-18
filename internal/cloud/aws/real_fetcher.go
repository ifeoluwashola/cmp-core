// internal/cloud/aws/real_fetcher.go
// Real AWS cloud provider — fetches live infrastructure via the AWS SDK v2.
//
// Authentication flow (cross-account AssumeRole):
//  1. Load the base AWS config from the environment (instance profile / env
//     vars / ~/.aws/credentials — whichever is found first).
//  2. If the CloudEnvironment has a RoleARN set, create a new config that
//     uses STS AssumeRole credentials scoped to that role.  This lets the
//     platform operate in read-only mode inside the client's AWS account
//     without needing long-lived keys.
//  3. Make real API calls using the assumed-role config.
//
// FetchCosts retrieves real billing telemetry from AWS Cost Explorer.
// Billing data is aggregated daily and grouped by service category.

package aws

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials/stscreds"
	"github.com/aws/aws-sdk-go-v2/service/costexplorer"
	cetypes "github.com/aws/aws-sdk-go-v2/service/costexplorer/types"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/aws/aws-sdk-go-v2/service/ecs"
	"github.com/aws/aws-sdk-go-v2/service/eks"
	"github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2"
	"github.com/aws/aws-sdk-go-v2/service/lambda"
	"github.com/aws/aws-sdk-go-v2/service/rds"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	"github.com/google/uuid"
	"github.com/ifeoluwashola/cmp-core/internal/models"
)

// RealFetcher implements cloud.Provider using the AWS SDK v2.
// It performs cross-account AssumeRole when the environment's RoleARN is set.
type RealFetcher struct{}

// NewRealFetcher constructs a RealFetcher.
func NewRealFetcher() *RealFetcher { return &RealFetcher{} }

// ─── FetchResources ────────────────────────────────────────────────────────────

// FetchResources discovers EC2 instances and S3 buckets across all regions
// configured for the environment. Results are aggregated and deduplicated by
// provider_resource_id so global resources (like S3 buckets) don't appear
// multiple times when several regions are configured.
func (f *RealFetcher) FetchResources(ctx context.Context, env models.CloudEnvironment) ([]models.InfrastructureResource, error) {
	if len(env.Regions) == 0 {
		env.Regions = []string{"us-east-1"}
	}

	seen := make(map[string]struct{})
	var resources []models.InfrastructureResource

	for _, region := range env.Regions {
		// Clone the environment with a single region for this iteration.
		envForRegion := env
		envForRegion.Regions = []string{region}

		cfg, err := f.buildConfig(ctx, envForRegion)
		if err != nil {
			fmt.Printf("aws real: build config env=%s region=%s: %v\n", env.ID, region, err)
			continue
		}

		// Helper to invoke a fetcher and merge its results
		var fetchErr error
		merge := func(fetcherName string, fn func(context.Context, aws.Config, models.CloudEnvironment) ([]models.InfrastructureResource, error)) {
			if fetchErr != nil {
				return
			}
			res, err := fn(ctx, cfg, env)
			if err != nil {
				fetchErr = fmt.Errorf("aws real: %s env=%s region=%s: %w", fetcherName, env.ID, region, err)
			} else {
				for _, r := range res {
					if _, dup := seen[r.ProviderResourceID]; !dup {
						seen[r.ProviderResourceID] = struct{}{}
						resources = append(resources, r)
					}
				}
			}
		}

		// ── Region-scoped resources ─────────────────────────────────────
		merge("fetchEC2Instances", f.fetchEC2Instances)
		merge("fetchNetworking", f.fetchNetworking)
		merge("fetchRDSInstances", f.fetchRDSInstances)
		merge("fetchLambdaFunctions", f.fetchLambdaFunctions)
		merge("fetchLoadBalancers", f.fetchLoadBalancers)
		merge("fetchEKSClusters", f.fetchEKSClusters)
		merge("fetchECSClusters", f.fetchECSClusters)

		if fetchErr != nil {
			return nil, fetchErr
		}

		// ── Global resources (only fetch once, on the first region) ────────
		if region == env.Regions[0] {
			merge("fetchS3Buckets", f.fetchS3Buckets)
			if fetchErr != nil {
				return nil, fetchErr
			}
		}
	}

	return resources, nil
}

// ─── FetchCosts ────────────────────────────────────────────────────────────────

// FetchCosts returns real unblended costs grouped by service from AWS Cost Explorer.
func (f *RealFetcher) FetchCosts(ctx context.Context, env models.CloudEnvironment) ([]models.DailyCost, error) {
	cfg, err := f.buildConfig(ctx, env)
	if err != nil {
		return nil, fmt.Errorf("FetchCosts: build config: %w", err)
	}

	client := costexplorer.NewFromConfig(cfg)
	
	now := time.Now().UTC()
	// CE dates are inclusive start and exclusive end. So Start: YYYY-MM-01, End: YYYY-MM-today.
	// If today is the 1st, Cost Explorer requires at least a 1-day range. We adjust End to tomorrow.
	start := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	end := now.AddDate(0, 0, 1)

	var costs []models.DailyCost

	out, err := client.GetCostAndUsage(ctx, &costexplorer.GetCostAndUsageInput{
		TimePeriod: &cetypes.DateInterval{
			Start: aws.String(start.Format("2006-01-02")),
			End:   aws.String(end.Format("2006-01-02")),
		},
		Granularity: cetypes.GranularityDaily,
		Metrics:     []string{"UnblendedCost"},
		GroupBy: []cetypes.GroupDefinition{
			{
				Type: cetypes.GroupDefinitionTypeDimension,
				Key:  aws.String("SERVICE"),
			},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("GetCostAndUsage: %w", err)
	}

	for _, result := range out.ResultsByTime {
		// Convert CE string date to time.Time
		if result.TimePeriod == nil || result.TimePeriod.Start == nil {
			continue
		}
		
		date, err := time.Parse("2006-01-02", *result.TimePeriod.Start)
		if err != nil {
			fmt.Printf("aws real: parse CE date %v: %v\n", *result.TimePeriod.Start, err)
			continue
		}

		for _, group := range result.Groups {
			if len(group.Keys) == 0 {
				continue
			}
			
			serviceCat := group.Keys[0]
			
			metrics, ok := group.Metrics["UnblendedCost"]
			if !ok || metrics.Amount == nil {
				continue
			}

			costs = append(costs, models.DailyCost{
				OrganizationID:  env.OrganizationID,
				EnvironmentID:   env.ID,
				Date:            date,
				ServiceCategory: serviceCat,
				Amount:          *metrics.Amount,
				Currency:        aws.ToString(metrics.Unit), // Usually USD
			})
		}
	}

	return costs, nil
}

// ─── internal helpers ──────────────────────────────────────────────────────────

// buildConfig constructs an aws.Config for the target environment.
// When a RoleARN is present it uses STS AssumeRole to obtain temporary
// credentials scoped to the client's account.
func (f *RealFetcher) buildConfig(ctx context.Context, env models.CloudEnvironment) (aws.Config, error) {
	// Determine the target region — use the first element of Regions.
	// For multi-region loops the caller slices env.Regions to a single entry.
	region := "us-east-1"
	if len(env.Regions) > 0 && env.Regions[0] != "" {
		region = env.Regions[0]
	}

	// Base config pinned to the target region.
	baseCfg, err := config.LoadDefaultConfig(ctx,
		config.WithRegion(region),
	)
	if err != nil {
		return aws.Config{}, fmt.Errorf("load default config: %w", err)
	}

	// No role ARN configured — use the base credentials directly.
	if env.RoleARN == nil || *env.RoleARN == "" {
		return baseCfg, nil
	}

	// Cross-account AssumeRole.
	// Validate the ARN before calling STS to avoid a confusing remote error
	// when the column holds a placeholder or garbage value.
	roleARN := *env.RoleARN
	if len(roleARN) < 20 || roleARN[:4] != "arn:" {
		// Log a warning and fall back to base credentials rather than crashing
		// the entire audit cycle for this environment.
		fmt.Printf("aws real: env %s has invalid role_arn %q — skipping AssumeRole, using base credentials\n",
			env.ID, roleARN)
		return baseCfg, nil
	}

	// Use the organization UUID as the session name so CloudTrail logs in
	// the client account are traceable back to the CMP tenant.
	stsClient := sts.NewFromConfig(baseCfg)
	provider := stscreds.NewAssumeRoleProvider(stsClient, roleARN, func(o *stscreds.AssumeRoleOptions) {
		o.RoleSessionName = "cmp-auditor-" + env.OrganizationID.String()[:8]
		// Uncomment the line below to enforce ExternalId matching the org UUID,
		// which prevents the Confused Deputy attack if your client CloudFormation
		// template passes an ExternalId.
		o.ExternalID = aws.String(env.OrganizationID.String())
	})

	assumedCfg, err := config.LoadDefaultConfig(ctx,
		config.WithCredentialsProvider(aws.NewCredentialsCache(provider)),
		config.WithRegion(region),
	)
	if err != nil {
		return aws.Config{}, fmt.Errorf("build assumed-role config for %s: %w", *env.RoleARN, err)
	}
	return assumedCfg, nil
}

// ─── resource fetchers ─────────────────────────────────────────────────────────

func (f *RealFetcher) fetchEC2Instances(ctx context.Context, cfg aws.Config, env models.CloudEnvironment) ([]models.InfrastructureResource, error) {
	client := ec2.NewFromConfig(cfg)
	now := time.Now().UTC()

	paginator := ec2.NewDescribeInstancesPaginator(client, &ec2.DescribeInstancesInput{})

	var resources []models.InfrastructureResource
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("DescribeInstances: %w", err)
		}

		for _, reservation := range page.Reservations {
			for _, inst := range reservation.Instances {
				if inst.InstanceId == nil {
					continue
				}

				attrs, err := json.Marshal(map[string]any{
					"region":            cfg.Region,
					"instance_type":     string(inst.InstanceType),
					"state":             stateString(inst.State),
					"private_ip":        aws.ToString(inst.PrivateIpAddress),
					"public_ip":         aws.ToString(inst.PublicIpAddress),
					"availability_zone": aws.ToString(inst.Placement.AvailabilityZone),
					"image_id":          aws.ToString(inst.ImageId),
					"launch_time":       inst.LaunchTime,
					"tags":              ec2TagsToMap(inst.Tags),
				})
				if err != nil {
					return nil, fmt.Errorf("marshal EC2 attrs: %w", err)
				}

				resources = append(resources, models.InfrastructureResource{
					ID:                 uuid.Nil,
					OrganizationID:     env.OrganizationID,
					EnvironmentID:      env.ID,
					ProviderResourceID: aws.ToString(inst.InstanceId),
					ResourceType:       "aws:ec2:instance",
					Attributes:         json.RawMessage(attrs),
					Status:             stateString(inst.State),
					LastAuditedAt:      &now,
				})
			}
		}
	}
	return resources, nil
}

func (f *RealFetcher) fetchNetworking(ctx context.Context, cfg aws.Config, env models.CloudEnvironment) ([]models.InfrastructureResource, error) {
	client := ec2.NewFromConfig(cfg)
	now := time.Now().UTC()
	var resources []models.InfrastructureResource

	// VPCs
	vpcPaginator := ec2.NewDescribeVpcsPaginator(client, &ec2.DescribeVpcsInput{})
	for vpcPaginator.HasMorePages() {
		page, err := vpcPaginator.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("DescribeVpcs: %w", err)
		}
		for _, vpc := range page.Vpcs {
			if vpc.VpcId == nil {
				continue
			}
			attrs, err := json.Marshal(map[string]any{
				"region":     cfg.Region,
				"cidr_block": aws.ToString(vpc.CidrBlock),
				"state":      string(vpc.State),
				"tags":       ec2TagsToMap(vpc.Tags),
			})
			if err == nil {
				resources = append(resources, models.InfrastructureResource{
					ID:                 uuid.Nil,
					OrganizationID:     env.OrganizationID,
					EnvironmentID:      env.ID,
					ProviderResourceID: aws.ToString(vpc.VpcId),
					ResourceType:       "aws:ec2:vpc",
					Attributes:         json.RawMessage(attrs),
					Status:             string(vpc.State),
					LastAuditedAt:      &now,
				})
			}
		}
	}

	// Subnets
	subnetPaginator := ec2.NewDescribeSubnetsPaginator(client, &ec2.DescribeSubnetsInput{})
	for subnetPaginator.HasMorePages() {
		page, err := subnetPaginator.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("DescribeSubnets: %w", err)
		}
		for _, subnet := range page.Subnets {
			if subnet.SubnetId == nil {
				continue
			}
			attrs, err := json.Marshal(map[string]any{
				"region":     cfg.Region,
				"vpc_id":     aws.ToString(subnet.VpcId),
				"cidr_block": aws.ToString(subnet.CidrBlock),
				"state":      string(subnet.State),
				"tags":       ec2TagsToMap(subnet.Tags),
			})
			if err == nil {
				resources = append(resources, models.InfrastructureResource{
					ID:                 uuid.Nil,
					OrganizationID:     env.OrganizationID,
					EnvironmentID:      env.ID,
					ProviderResourceID: aws.ToString(subnet.SubnetId),
					ResourceType:       "aws:ec2:subnet",
					Attributes:         json.RawMessage(attrs),
					Status:             string(subnet.State),
					LastAuditedAt:      &now,
				})
			}
		}
	}

	// NAT Gateways
	natPaginator := ec2.NewDescribeNatGatewaysPaginator(client, &ec2.DescribeNatGatewaysInput{})
	for natPaginator.HasMorePages() {
		page, err := natPaginator.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("DescribeNatGateways: %w", err)
		}
		for _, nat := range page.NatGateways {
			if nat.NatGatewayId == nil {
				continue
			}
			var publicIps []string
			for _, addr := range nat.NatGatewayAddresses {
				if addr.PublicIp != nil {
					publicIps = append(publicIps, aws.ToString(addr.PublicIp))
				}
			}
			attrs, err := json.Marshal(map[string]any{
				"region":     cfg.Region,
				"vpc_id":     aws.ToString(nat.VpcId),
				"state":      string(nat.State),
				"public_ips": publicIps,
				"tags":       ec2TagsToMap(nat.Tags),
			})
			if err == nil {
				resources = append(resources, models.InfrastructureResource{
					ID:                 uuid.Nil,
					OrganizationID:     env.OrganizationID,
					EnvironmentID:      env.ID,
					ProviderResourceID: aws.ToString(nat.NatGatewayId),
					ResourceType:       "aws:ec2:natgateway",
					Attributes:         json.RawMessage(attrs),
					Status:             string(nat.State),
					LastAuditedAt:      &now,
				})
			}
		}
	}

	// Elastic IPs
	out, err := client.DescribeAddresses(ctx, &ec2.DescribeAddressesInput{})
	if err != nil {
		return nil, fmt.Errorf("DescribeAddresses: %w", err)
	}
	for _, addr := range out.Addresses {
		pid := aws.ToString(addr.AllocationId)
		if pid == "" {
			pid = aws.ToString(addr.PublicIp)
		}
		if pid == "" {
			continue
		}
		attrs, err := json.Marshal(map[string]any{
			"region":               cfg.Region,
			"public_ip":            aws.ToString(addr.PublicIp),
			"instance_id":          aws.ToString(addr.InstanceId),
			"network_interface_id": aws.ToString(addr.NetworkInterfaceId),
			"tags":                 ec2TagsToMap(addr.Tags),
		})
		status := "unattached"
		if addr.InstanceId != nil || addr.NetworkInterfaceId != nil {
			status = "attached"
		}
		if err == nil {
			resources = append(resources, models.InfrastructureResource{
				ID:                 uuid.Nil,
				OrganizationID:     env.OrganizationID,
				EnvironmentID:      env.ID,
				ProviderResourceID: pid,
				ResourceType:       "aws:ec2:eip",
				Attributes:         json.RawMessage(attrs),
				Status:             status,
				LastAuditedAt:      &now,
			})
		}
	}

	return resources, nil
}

func (f *RealFetcher) fetchRDSInstances(ctx context.Context, cfg aws.Config, env models.CloudEnvironment) ([]models.InfrastructureResource, error) {
	client := rds.NewFromConfig(cfg)
	now := time.Now().UTC()
	var resources []models.InfrastructureResource

	paginator := rds.NewDescribeDBInstancesPaginator(client, &rds.DescribeDBInstancesInput{})
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("DescribeDBInstances: %w", err)
		}
		for _, db := range page.DBInstances {
			if db.DBInstanceArn == nil {
				continue
			}
			attrs, err := json.Marshal(map[string]any{
				"region":             cfg.Region,
				"engine":             aws.ToString(db.Engine),
				"db_instance_status": aws.ToString(db.DBInstanceStatus),
				"db_instance_class":  aws.ToString(db.DBInstanceClass),
			})
			if err == nil {
				resources = append(resources, models.InfrastructureResource{
					ID:                 uuid.Nil,
					OrganizationID:     env.OrganizationID,
					EnvironmentID:      env.ID,
					ProviderResourceID: aws.ToString(db.DBInstanceArn),
					ResourceType:       "aws:rds:dbinstance",
					Attributes:         json.RawMessage(attrs),
					Status:             aws.ToString(db.DBInstanceStatus),
					LastAuditedAt:      &now,
				})
			}
		}
	}
	return resources, nil
}

func (f *RealFetcher) fetchLambdaFunctions(ctx context.Context, cfg aws.Config, env models.CloudEnvironment) ([]models.InfrastructureResource, error) {
	client := lambda.NewFromConfig(cfg)
	now := time.Now().UTC()
	var resources []models.InfrastructureResource

	paginator := lambda.NewListFunctionsPaginator(client, &lambda.ListFunctionsInput{})
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("ListFunctions: %w", err)
		}
		for _, fn := range page.Functions {
			if fn.FunctionArn == nil {
				continue
			}
			attrs, err := json.Marshal(map[string]any{
				"region":      cfg.Region,
				"runtime":     string(fn.Runtime),
				"memory_size": fn.MemorySize,
				"state":       string(fn.State),
			})
			if err == nil {
				resources = append(resources, models.InfrastructureResource{
					ID:                 uuid.Nil,
					OrganizationID:     env.OrganizationID,
					EnvironmentID:      env.ID,
					ProviderResourceID: aws.ToString(fn.FunctionArn),
					ResourceType:       "aws:lambda:function",
					Attributes:         json.RawMessage(attrs),
					Status:             string(fn.State),
					LastAuditedAt:      &now,
				})
			}
		}
	}
	return resources, nil
}

func (f *RealFetcher) fetchLoadBalancers(ctx context.Context, cfg aws.Config, env models.CloudEnvironment) ([]models.InfrastructureResource, error) {
	client := elasticloadbalancingv2.NewFromConfig(cfg)
	now := time.Now().UTC()
	var resources []models.InfrastructureResource

	paginator := elasticloadbalancingv2.NewDescribeLoadBalancersPaginator(client, &elasticloadbalancingv2.DescribeLoadBalancersInput{})
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("DescribeLoadBalancers: %w", err)
		}
		for _, lb := range page.LoadBalancers {
			if lb.LoadBalancerArn == nil {
				continue
			}
			status := "unknown"
			if lb.State != nil && lb.State.Code != "" {
				status = string(lb.State.Code)
			}
			attrs, err := json.Marshal(map[string]any{
				"region": cfg.Region,
				"type":   string(lb.Type),
				"state":  status,
				"vpc_id": aws.ToString(lb.VpcId),
			})
			if err == nil {
				resources = append(resources, models.InfrastructureResource{
					ID:                 uuid.Nil,
					OrganizationID:     env.OrganizationID,
					EnvironmentID:      env.ID,
					ProviderResourceID: aws.ToString(lb.LoadBalancerArn),
					ResourceType:       "aws:elasticloadbalancingv2:loadbalancer",
					Attributes:         json.RawMessage(attrs),
					Status:             status,
					LastAuditedAt:      &now,
				})
			}
		}
	}
	return resources, nil
}

func (f *RealFetcher) fetchEKSClusters(ctx context.Context, cfg aws.Config, env models.CloudEnvironment) ([]models.InfrastructureResource, error) {
	client := eks.NewFromConfig(cfg)
	now := time.Now().UTC()
	var resources []models.InfrastructureResource

	paginator := eks.NewListClustersPaginator(client, &eks.ListClustersInput{})
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("ListClusters: %w", err)
		}
		for _, name := range page.Clusters {
			out, err := client.DescribeCluster(ctx, &eks.DescribeClusterInput{Name: aws.String(name)})
			if err != nil {
				fmt.Printf("aws real: DescribeCluster %s env=%s: %v\n", name, env.ID, err)
				continue
			}
			cluster := out.Cluster
			if cluster == nil || cluster.Arn == nil {
				continue
			}

			attrs, err := json.Marshal(map[string]any{
				"region":  cfg.Region,
				"version": aws.ToString(cluster.Version),
				"status":  string(cluster.Status),
			})
			if err == nil {
				resources = append(resources, models.InfrastructureResource{
					ID:                 uuid.Nil,
					OrganizationID:     env.OrganizationID,
					EnvironmentID:      env.ID,
					ProviderResourceID: aws.ToString(cluster.Arn),
					ResourceType:       "aws:eks:cluster",
					Attributes:         json.RawMessage(attrs),
					Status:             string(cluster.Status),
					LastAuditedAt:      &now,
				})
			}
		}
	}
	return resources, nil
}

func (f *RealFetcher) fetchECSClusters(ctx context.Context, cfg aws.Config, env models.CloudEnvironment) ([]models.InfrastructureResource, error) {
	client := ecs.NewFromConfig(cfg)
	now := time.Now().UTC()
	var resources []models.InfrastructureResource

	paginator := ecs.NewListClustersPaginator(client, &ecs.ListClustersInput{})
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("ListClusters: %w", err)
		}
		for _, arn := range page.ClusterArns {
			attrs, err := json.Marshal(map[string]any{
				"region": cfg.Region,
			})
			if err == nil {
				resources = append(resources, models.InfrastructureResource{
					ID:                 uuid.Nil,
					OrganizationID:     env.OrganizationID,
					EnvironmentID:      env.ID,
					ProviderResourceID: arn,
					ResourceType:       "aws:ecs:cluster",
					Attributes:         json.RawMessage(attrs),
					Status:             "active", // ECS ListClusters only returns active clusters by default
					LastAuditedAt:      &now,
				})
			}
		}
	}
	return resources, nil
}

func (f *RealFetcher) fetchS3Buckets(ctx context.Context, cfg aws.Config, env models.CloudEnvironment) ([]models.InfrastructureResource, error) {
	client := s3.NewFromConfig(cfg)
	now := time.Now().UTC()

	out, err := client.ListBuckets(ctx, &s3.ListBucketsInput{})
	if err != nil {
		return nil, fmt.Errorf("ListBuckets: %w", err)
	}

	var resources []models.InfrastructureResource
	for _, bucket := range out.Buckets {
		if bucket.Name == nil {
			continue
		}

		attrs, err := json.Marshal(map[string]any{
			"region":        cfg.Region,
			"creation_date": bucket.CreationDate,
		})
		if err != nil {
			return nil, fmt.Errorf("marshal S3 attrs: %w", err)
		}

		resources = append(resources, models.InfrastructureResource{
			ID:                 uuid.Nil,
			OrganizationID:     env.OrganizationID,
			EnvironmentID:      env.ID,
			ProviderResourceID: aws.ToString(bucket.Name),
			ResourceType:       "aws:s3:bucket",
			Attributes:         json.RawMessage(attrs),
			Status:             "available",
			LastAuditedAt:      &now,
		})
	}
	return resources, nil
}

// ─── tiny helpers ──────────────────────────────────────────────────────────────

func stateString(state *ec2types.InstanceState) string {
	if state == nil {
		return "unknown"
	}
	return string(state.Name)
}

func ec2TagsToMap(tags []ec2types.Tag) map[string]string {
	m := make(map[string]string, len(tags))
	for _, t := range tags {
		m[aws.ToString(t.Key)] = aws.ToString(t.Value)
	}
	return m
}
