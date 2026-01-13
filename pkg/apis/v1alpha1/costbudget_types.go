package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// CostBudget defines a spending limit with alerts and enforcement
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster,shortName=cb
// +kubebuilder:printcolumn:name="Monthly Limit",type=string,JSONPath=`.spec.monthlyLimit`
// +kubebuilder:printcolumn:name="Current Spend",type=string,JSONPath=`.status.currentSpend`
// +kubebuilder:printcolumn:name="Percentage",type=string,JSONPath=`.status.percentageUsed`
// +kubebuilder:printcolumn:name="Status",type=string,JSONPath=`.status.budgetStatus`
type CostBudget struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   CostBudgetSpec   `json:"spec,omitempty"`
	Status CostBudgetStatus `json:"status,omitempty"`
}

// CostBudgetSpec defines the desired budget configuration
type CostBudgetSpec struct {
	// MonthlyLimit is the maximum spend allowed per month in USD
	// +kubebuilder:validation:Minimum=0
	MonthlyLimit float64 `json:"monthlyLimit"`

	// Scope defines what resources this budget applies to
	Scope BudgetScope `json:"scope"`

	// Alerts defines threshold-based alerting configuration
	// +optional
	Alerts []BudgetAlert `json:"alerts,omitempty"`

	// Enforcement defines what happens when budget is exceeded
	// +optional
	Enforcement *BudgetEnforcement `json:"enforcement,omitempty"`

	// StartDate defines when the budget period starts (defaults to current month)
	// +optional
	StartDate *metav1.Time `json:"startDate,omitempty"`

	// Enabled allows temporarily disabling budget without deletion
	// +kubebuilder:default=true
	Enabled bool `json:"enabled"`
}

// BudgetScope defines what resources the budget applies to
type BudgetScope struct {
	// Namespaces limits budget to specific namespaces (empty = all)
	// +optional
	Namespaces []string `json:"namespaces,omitempty"`

	// Labels limits budget to pods with these labels
	// +optional
	Labels map[string]string `json:"labels,omitempty"`

	// ExperimentID limits budget to specific experiment tracking IDs
	// +optional
	ExperimentID string `json:"experimentID,omitempty"`

	// Teams limits budget to specific team identifiers
	// +optional
	Teams []string `json:"teams,omitempty"`
}

// BudgetAlert defines a threshold-based alert
type BudgetAlert struct {
	// Name is a human-readable identifier for this alert
	Name string `json:"name"`

	// ThresholdPercent triggers alert when spend reaches this percentage (e.g., 80)
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=100
	ThresholdPercent float64 `json:"thresholdPercent"`

	// Channels defines where to send alerts (email, slack, pagerduty, webhook)
	Channels []AlertChannel `json:"channels"`

	// Severity defines alert priority (info, warning, critical)
	// +kubebuilder:validation:Enum=info;warning;critical
	// +kubebuilder:default=warning
	Severity string `json:"severity"`
}

// AlertChannel defines a notification destination
type AlertChannel struct {
	// Type is the channel type (email, slack, pagerduty, webhook)
	// +kubebuilder:validation:Enum=email;slack;pagerduty;webhook
	Type string `json:"type"`

	// Config contains channel-specific configuration (e.g., webhook URL, email address)
	// +optional
	Config map[string]string `json:"config,omitempty"`

	// ConfigSecretRefs allows referencing sensitive config values from secrets
	// +optional
	ConfigSecretRefs map[string]corev1.SecretKeySelector `json:"configSecretRefs,omitempty"`
}

// BudgetEnforcement defines actions when budget is exceeded
type BudgetEnforcement struct {
	// Action defines what to do when budget exceeded (alert, throttle, block)
	// - alert: Only send notifications
	// - throttle: Reduce resource allocation (scale down spot instances)
	// - block: Prevent new GPU pod creation
	// +kubebuilder:validation:Enum=alert;throttle;block
	// +kubebuilder:default=alert
	Action string `json:"action"`

	// GracePeriodMinutes allows exceeding budget temporarily
	// +kubebuilder:default=60
	GracePeriodMinutes int `json:"gracePeriodMinutes"`

	// ThrottleConfig defines throttling behavior
	// +optional
	ThrottleConfig *ThrottleConfig `json:"throttleConfig,omitempty"`
}

// ThrottleConfig defines how to reduce spending
type ThrottleConfig struct {
	// MaxSpotInstances limits spot GPU nodes when throttling
	// +optional
	MaxSpotInstances *int `json:"maxSpotInstances,omitempty"`

	// BlockSpotCreation prevents new spot instance launches
	// +kubebuilder:default=false
	BlockSpotCreation bool `json:"blockSpotCreation"`

	// PreferOnDemand switches to on-demand over spot when throttling
	// +kubebuilder:default=false
	PreferOnDemand bool `json:"preferOnDemand"`
}

// CostBudgetStatus represents the current budget state
type CostBudgetStatus struct {
	// CurrentSpend is the total spend for the current period (USD)
	CurrentSpend float64 `json:"currentSpend"`

	// PercentageUsed is current spend / monthly limit * 100
	PercentageUsed float64 `json:"percentageUsed"`

	// BudgetStatus indicates the current state (ok, warning, exceeded)
	// +kubebuilder:validation:Enum=ok;warning;exceeded
	BudgetStatus string `json:"budgetStatus"`

	// ProjectedMonthlySpend estimates total month spend based on current rate
	ProjectedMonthlySpend float64 `json:"projectedMonthlySpend"`

	// DaysRemaining in the current budget period
	DaysRemaining int `json:"daysRemaining"`

	// AlertsFired tracks which alerts have been triggered
	AlertsFired []AlertFired `json:"alertsFired,omitempty"`

	// EnforcementActive indicates if enforcement actions are in effect
	EnforcementActive bool `json:"enforcementActive"`

	// ExceededSince tracks when budget first exceeded 100% (for grace period)
	// +optional
	ExceededSince *metav1.Time `json:"exceededSince,omitempty"`

	// LastUpdated is when the status was last calculated
	LastUpdated metav1.Time `json:"lastUpdated"`

	// Breakdown shows spend by category
	Breakdown CostBreakdown `json:"breakdown,omitempty"`
}

// AlertFired records an alert that was triggered
type AlertFired struct {
	// AlertName is the name of the alert
	AlertName string `json:"alertName"`

	// Timestamp when the alert fired
	Timestamp metav1.Time `json:"timestamp"`

	// Threshold percentage that triggered the alert
	Threshold float64 `json:"threshold"`

	// Acknowledged indicates if alert has been acked by user
	Acknowledged bool `json:"acknowledged"`
}

// CostBreakdown categorizes spending
type CostBreakdown struct {
	// ByNamespace shows spend per namespace
	ByNamespace map[string]float64 `json:"byNamespace,omitempty"`

	// ByLabel shows spend per label value
	ByLabel map[string]float64 `json:"byLabel,omitempty"`

	// ByInstanceType shows spend per GPU type
	ByInstanceType map[string]float64 `json:"byInstanceType,omitempty"`

	// SpotVsOnDemand shows spot vs on-demand costs
	SpotVsOnDemand map[string]float64 `json:"spotVsOnDemand,omitempty"`
}

// +kubebuilder:object:root=true

// CostBudgetList contains a list of CostBudget
type CostBudgetList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []CostBudget `json:"items"`
}

func init() {
	SchemeBuilder.Register(&CostBudget{}, &CostBudgetList{})
}
