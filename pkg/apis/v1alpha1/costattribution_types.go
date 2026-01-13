package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// CostAttribution tracks GPU spending for attribution and analysis
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster,shortName=ca
// +kubebuilder:printcolumn:name="Namespace",type=string,JSONPath=`.spec.namespace`
// +kubebuilder:printcolumn:name="Daily Cost",type=string,JSONPath=`.status.dailyCost`
// +kubebuilder:printcolumn:name="Monthly Cost",type=string,JSONPath=`.status.monthlyCost`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`
type CostAttribution struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   CostAttributionSpec   `json:"spec,omitempty"`
	Status CostAttributionStatus `json:"status,omitempty"`
}

// CostAttributionSpec defines what to track
type CostAttributionSpec struct {
	// Namespace to track costs for
	// +optional
	Namespace string `json:"namespace,omitempty"`

	// Labels to filter and track pods by
	// +optional
	Labels map[string]string `json:"labels,omitempty"`

	// ExperimentID for ML experiment tracking integration
	// +optional
	ExperimentID string `json:"experimentID,omitempty"`

	// Team identifier for chargeback
	// +optional
	Team string `json:"team,omitempty"`

	// Project identifier for grouping experiments
	// +optional
	Project string `json:"project,omitempty"`

	// CostCenter for financial reporting
	// +optional
	CostCenter string `json:"costCenter,omitempty"`

	// Tags are arbitrary key-value pairs for custom attribution
	// +optional
	Tags map[string]string `json:"tags,omitempty"`

	// RetentionDays defines how long to keep historical data (default: 90)
	// +kubebuilder:default=90
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=365
	RetentionDays int `json:"retentionDays"`
}

// CostAttributionStatus tracks current and historical costs
type CostAttributionStatus struct {
	// TotalCost is lifetime spend for this attribution (USD)
	TotalCost float64 `json:"totalCost"`

	// DailyCost is the current day's spend (USD)
	DailyCost float64 `json:"dailyCost"`

	// MonthlyCost is the current month's spend (USD)
	MonthlyCost float64 `json:"monthlyCost"`

	// HourlyCost is the current hourly rate (USD/hour)
	HourlyCost float64 `json:"hourlyCost"`

	// ActivePods currently consuming GPU resources
	ActivePods int `json:"activePods"`

	// ActiveGPUs currently in use
	ActiveGPUs int `json:"activeGPUs"`

	// GPUHours tracks total GPU time consumed
	GPUHours float64 `json:"gpuHours"`

	// CostPerGPUHour is the average cost efficiency
	CostPerGPUHour float64 `json:"costPerGPUHour"`

	// LastUpdated timestamp
	LastUpdated metav1.Time `json:"lastUpdated"`

	// Breakdown provides detailed cost attribution
	DetailedBreakdown DetailedBreakdown `json:"detailedBreakdown,omitempty"`

	// HistoricalData stores time-series cost data
	HistoricalData []CostDataPoint `json:"historicalData,omitempty"`

	// Savings tracks optimization impact
	Savings SavingsData `json:"savings,omitempty"`
}

// DetailedBreakdown provides granular cost visibility
type DetailedBreakdown struct {
	// ByPod shows cost per pod
	ByPod map[string]PodCostInfo `json:"byPod,omitempty"`

	// ByGPUType shows cost per GPU model
	ByGPUType map[string]float64 `json:"byGPUType,omitempty"`

	// ByCapacityType shows spot vs on-demand costs
	ByCapacityType map[string]float64 `json:"byCapacityType,omitempty"`

	// ByNode shows cost per node
	ByNode map[string]float64 `json:"byNode,omitempty"`

	// ByHour shows hourly cost distribution (last 24h)
	ByHour []HourlyCost `json:"byHour,omitempty"`

	// ByDay shows daily cost distribution (last 30d)
	ByDay []DailyCost `json:"byDay,omitempty"`
}

// PodCostInfo tracks per-pod cost details
type PodCostInfo struct {
	// PodName is the pod identifier
	PodName string `json:"podName"`

	// GPUType is the GPU model used (e.g., "nvidia-tesla-a100")
	GPUType string `json:"gpuType"`

	// GPUCount is number of GPUs allocated
	GPUCount int `json:"gpuCount"`

	// StartTime when pod started consuming GPUs
	StartTime metav1.Time `json:"startTime"`

	// Cost is total spend for this pod (USD)
	Cost float64 `json:"cost"`

	// HourlyRate is the cost per hour (USD/hr)
	HourlyRate float64 `json:"hourlyRate"`

	// CapacityType is spot/on-demand/reserved
	CapacityType string `json:"capacityType"`

	// SharingMode indicates MIG/MPS/time-slicing/exclusive
	SharingMode string `json:"sharingMode,omitempty"`

	// Node where pod is running
	Node string `json:"node"`
}

// HourlyCost represents cost for a specific hour
type HourlyCost struct {
	// Timestamp for this hour
	Timestamp metav1.Time `json:"timestamp"`

	// Cost in USD
	Cost float64 `json:"cost"`

	// GPUHours consumed
	GPUHours float64 `json:"gpuHours"`
}

// DailyCost represents cost for a specific day
type DailyCost struct {
	// Date in YYYY-MM-DD format
	Date string `json:"date"`

	// Cost in USD
	Cost float64 `json:"cost"`

	// GPUHours consumed
	GPUHours float64 `json:"gpuHours"`

	// PeakGPUs is max concurrent GPUs used
	PeakGPUs int `json:"peakGPUs"`
}

// CostDataPoint is a time-series data point
type CostDataPoint struct {
	// Timestamp of the measurement
	Timestamp metav1.Time `json:"timestamp"`

	// Cost at this point (USD)
	Cost float64 `json:"cost"`

	// Rate is cost per hour at this point (USD/hr)
	Rate float64 `json:"rate"`

	// GPUs in use at this point
	GPUs int `json:"gpus"`

	// Pods running at this point
	Pods int `json:"pods"`
}

// SavingsData tracks cost optimization impact
type SavingsData struct {
	// TotalSavings from all optimizations (USD)
	TotalSavings float64 `json:"totalSavings"`

	// SpotSavings from using spot instances
	SpotSavings float64 `json:"spotSavings"`

	// SharingSavings from MIG/MPS/time-slicing
	SharingSavings float64 `json:"sharingSavings"`

	// AutoscalingSavings from dynamic scaling
	AutoscalingSavings float64 `json:"autoscalingSavings"`

	// WasteEliminated is cost avoided by waste detection
	WasteEliminated float64 `json:"wasteEliminated"`

	// SavingsPercentage is total savings / theoretical on-demand cost * 100
	SavingsPercentage float64 `json:"savingsPercentage"`

	// BaselineCost is what spending would be without optimization
	BaselineCost float64 `json:"baselineCost"`
}

// +kubebuilder:object:root=true

// CostAttributionList contains a list of CostAttribution
type CostAttributionList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []CostAttribution `json:"items"`
}

func init() {
	SchemeBuilder.Register(&CostAttribution{}, &CostAttributionList{})
}
