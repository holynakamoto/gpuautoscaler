package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// AutoscalingPolicySpec defines the desired state of AutoscalingPolicy
type AutoscalingPolicySpec struct {
	// Enabled enables or disables autoscaling
	// +optional
	// +kubebuilder:default=true
	Enabled bool `json:"enabled,omitempty"`

	// Provider specifies the cloud provider
	// +kubebuilder:validation:Enum=aws;gcp;azure;custom
	Provider string `json:"provider"`

	// ScaleUpThreshold is the GPU utilization threshold to trigger scale-up (0-1)
	// +optional
	// +kubebuilder:default=0.8
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=1
	ScaleUpThreshold float64 `json:"scaleUpThreshold,omitempty"`

	// ScaleDownThreshold is the GPU utilization threshold to trigger scale-down (0-1)
	// +optional
	// +kubebuilder:default=0.2
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=1
	ScaleDownThreshold float64 `json:"scaleDownThreshold,omitempty"`

	// ScaleUpCooldownSeconds is the cooldown period after scale-up in seconds
	// +optional
	// +kubebuilder:default=180
	// +kubebuilder:validation:Minimum=0
	ScaleUpCooldownSeconds int32 `json:"scaleUpCooldownSeconds,omitempty"`

	// ScaleDownCooldownSeconds is the cooldown period after scale-down in seconds
	// +optional
	// +kubebuilder:default=600
	// +kubebuilder:validation:Minimum=0
	ScaleDownCooldownSeconds int32 `json:"scaleDownCooldownSeconds,omitempty"`

	// PendingPodTimeoutSeconds is the time to wait before scaling up for pending pods
	// +optional
	// +kubebuilder:default=120
	// +kubebuilder:validation:Minimum=0
	PendingPodTimeoutSeconds int32 `json:"pendingPodTimeoutSeconds,omitempty"`

	// MinNodes is the minimum number of GPU nodes
	// +optional
	// +kubebuilder:default=0
	// +kubebuilder:validation:Minimum=0
	MinNodes int32 `json:"minNodes,omitempty"`

	// MaxNodes is the maximum number of GPU nodes
	// +optional
	// +kubebuilder:default=100
	// +kubebuilder:validation:Minimum=1
	MaxNodes int32 `json:"maxNodes,omitempty"`

	// SpotInstancePercentage is the target percentage of spot instances (0-1)
	// +optional
	// +kubebuilder:default=0.6
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=1
	SpotInstancePercentage float64 `json:"spotInstancePercentage,omitempty"`

	// EnableSpotInstances enables spot instance orchestration
	// +optional
	// +kubebuilder:default=true
	EnableSpotInstances bool `json:"enableSpotInstances,omitempty"`

	// EnableMultiTierScaling enables multi-tier scaling (spot → on-demand → reserved)
	// +optional
	// +kubebuilder:default=true
	EnableMultiTierScaling bool `json:"enableMultiTierScaling,omitempty"`

	// EnablePredictiveScaling enables predictive scaling based on historical patterns
	// +optional
	// +kubebuilder:default=false
	EnablePredictiveScaling bool `json:"enablePredictiveScaling,omitempty"`

	// NodePools defines the GPU node pools to manage
	// +optional
	NodePools []NodePoolSpec `json:"nodePools,omitempty"`

	// NodeSelector specifies which nodes this policy applies to
	// +optional
	NodeSelector map[string]string `json:"nodeSelector,omitempty"`
}

// NodePoolSpec defines a GPU node pool
type NodePoolSpec struct {
	// Name is the node pool name
	Name string `json:"name"`

	// MinSize is the minimum number of nodes in this pool
	// +optional
	// +kubebuilder:default=0
	MinSize int32 `json:"minSize,omitempty"`

	// MaxSize is the maximum number of nodes in this pool
	// +optional
	// +kubebuilder:default=100
	MaxSize int32 `json:"maxSize,omitempty"`

	// GPUType specifies the GPU type (e.g., "nvidia-tesla-v100", "nvidia-tesla-t4", "nvidia-a100")
	// +optional
	GPUType string `json:"gpuType,omitempty"`

	// InstanceTypes lists the instance types to use (e.g., ["p3.2xlarge", "p3.8xlarge"])
	// +optional
	InstanceTypes []string `json:"instanceTypes,omitempty"`

	// CapacityType specifies the capacity type
	// +kubebuilder:validation:Enum=spot;on-demand;reserved
	CapacityType string `json:"capacityType"`

	// SpotPercentage is the target percentage of spot instances in this pool (0-1)
	// +optional
	// +kubebuilder:default=0.6
	SpotPercentage float64 `json:"spotPercentage,omitempty"`

	// Priority defines the pool priority (higher values = higher priority)
	// +optional
	// +kubebuilder:default=0
	Priority int32 `json:"priority,omitempty"`

	// Labels to apply to nodes in this pool
	// +optional
	Labels map[string]string `json:"labels,omitempty"`

	// Taints to apply to nodes in this pool
	// +optional
	Taints []corev1.Taint `json:"taints,omitempty"`

	// AvailabilityZones specifies which zones to use
	// +optional
	AvailabilityZones []string `json:"availabilityZones,omitempty"`
}

// AutoscalingPolicyStatus defines the observed state of AutoscalingPolicy
type AutoscalingPolicyStatus struct {
	// CurrentNodes is the current number of GPU nodes
	CurrentNodes int32 `json:"currentNodes"`

	// DesiredNodes is the desired number of GPU nodes
	DesiredNodes int32 `json:"desiredNodes"`

	// SpotNodes is the current number of spot instance nodes
	SpotNodes int32 `json:"spotNodes"`

	// OnDemandNodes is the current number of on-demand nodes
	OnDemandNodes int32 `json:"onDemandNodes"`

	// ReservedNodes is the current number of reserved instance nodes
	ReservedNodes int32 `json:"reservedNodes"`

	// AverageGPUUtilization is the current average GPU utilization (0-1)
	AverageGPUUtilization float64 `json:"averageGPUUtilization"`

	// PendingPods is the current number of pending GPU pods
	PendingPods int32 `json:"pendingPods"`

	// LastScaleUpTime is the timestamp of the last scale-up action
	// +optional
	LastScaleUpTime *metav1.Time `json:"lastScaleUpTime,omitempty"`

	// LastScaleDownTime is the timestamp of the last scale-down action
	// +optional
	LastScaleDownTime *metav1.Time `json:"lastScaleDownTime,omitempty"`

	// LastScalingAction is the most recent scaling action
	// +optional
	LastScalingAction string `json:"lastScalingAction,omitempty"`

	// LastScalingReason is the reason for the last scaling action
	// +optional
	LastScalingReason string `json:"lastScalingReason,omitempty"`

	// SpotInterruptions is the number of spot interruptions in the last 24 hours
	SpotInterruptions int32 `json:"spotInterruptions"`

	// EstimatedMonthlyCost is the estimated monthly cost in USD
	// +optional
	EstimatedMonthlyCost float64 `json:"estimatedMonthlyCost,omitempty"`

	// EstimatedMonthlySavings is the estimated monthly savings from using spot instances
	// +optional
	EstimatedMonthlySavings float64 `json:"estimatedMonthlySavings,omitempty"`

	// PredictiveScaling contains predictive scaling information
	// +optional
	PredictiveScaling *PredictiveScalingStatus `json:"predictiveScaling,omitempty"`

	// Conditions represent the latest available observations of the policy's state
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// PredictiveScalingStatus contains predictive scaling information
type PredictiveScalingStatus struct {
	// Enabled indicates if predictive scaling is active
	Enabled bool `json:"enabled"`

	// PredictedUtilization is the predicted GPU utilization (0-1)
	PredictedUtilization float64 `json:"predictedUtilization"`

	// RecommendedNodes is the recommended number of nodes based on prediction
	RecommendedNodes int32 `json:"recommendedNodes"`

	// Confidence is the prediction confidence level (0-1)
	Confidence float64 `json:"confidence"`

	// NextBusyPeriod is the predicted next busy period
	// +optional
	NextBusyPeriod *metav1.Time `json:"nextBusyPeriod,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster,shortName=asp
// +kubebuilder:printcolumn:name="Provider",type=string,JSONPath=`.spec.provider`
// +kubebuilder:printcolumn:name="Current",type=integer,JSONPath=`.status.currentNodes`
// +kubebuilder:printcolumn:name="Desired",type=integer,JSONPath=`.status.desiredNodes`
// +kubebuilder:printcolumn:name="Min",type=integer,JSONPath=`.spec.minNodes`
// +kubebuilder:printcolumn:name="Max",type=integer,JSONPath=`.spec.maxNodes`
// +kubebuilder:printcolumn:name="Utilization",type=string,JSONPath=`.status.averageGPUUtilization`
// +kubebuilder:printcolumn:name="Spot %",type=string,JSONPath=`.spec.spotInstancePercentage`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// AutoscalingPolicy is the Schema for the autoscalingpolicies API
type AutoscalingPolicy struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   AutoscalingPolicySpec   `json:"spec,omitempty"`
	Status AutoscalingPolicyStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// AutoscalingPolicyList contains a list of AutoscalingPolicy
type AutoscalingPolicyList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []AutoscalingPolicy `json:"items"`
}

func init() {
	SchemeBuilder.Register(&AutoscalingPolicy{}, &AutoscalingPolicyList{})
}
