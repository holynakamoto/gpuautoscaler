package autoscaler

import (
	"context"
	"time"
)

// CloudProvider is the interface for cloud provider integration
type CloudProvider interface {
	// ScaleUp adds nodes to a node pool
	ScaleUp(ctx context.Context, nodePool *NodePoolConfig, count int) error

	// ScaleDown removes a node from the cluster
	ScaleDown(ctx context.Context, nodeName string) error

	// GetSpotTerminationNotice checks if a spot instance has a termination notice
	GetSpotTerminationNotice(ctx context.Context, nodeName string) (time.Time, bool, error)

	// GetSpotPrice returns current spot price for an instance type
	GetSpotPrice(ctx context.Context, instanceType string) (float64, error)

	// GetOnDemandPrice returns on-demand price for an instance type
	GetOnDemandPrice(ctx context.Context, instanceType string) (float64, error)

	// GetRecommendedSpotInstanceTypes returns instance types suitable for spot
	GetRecommendedSpotInstanceTypes(ctx context.Context) ([]string, error)

	// GetAvailabilityZones returns available zones with low spot interruption
	GetAvailabilityZones(ctx context.Context) ([]string, error)

	// GetNodePoolInfo returns information about a node pool
	GetNodePoolInfo(ctx context.Context, nodePoolName string) (*NodePoolInfo, error)
}

// NodePoolInfo contains information about a cloud provider node pool
type NodePoolInfo struct {
	Name          string
	CurrentSize   int
	MinSize       int
	MaxSize       int
	InstanceType  string
	CapacityType  string
	AvailableGPUs int
	Cost          float64
}
