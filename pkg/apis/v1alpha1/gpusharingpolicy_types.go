package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// GPUSharingPolicySpec defines the desired state of GPUSharingPolicy
type GPUSharingPolicySpec struct {
	// Strategy defines the GPU sharing strategy
	// +kubebuilder:validation:Enum=mig;mps;timeslicing;exclusive;auto
	Strategy string `json:"strategy"`

	// NodeSelector specifies which nodes this policy applies to
	// +optional
	NodeSelector map[string]string `json:"nodeSelector,omitempty"`

	// NamespaceSelector specifies which namespaces this policy applies to
	// +optional
	NamespaceSelector *metav1.LabelSelector `json:"namespaceSelector,omitempty"`

	// PodSelector specifies which pods this policy applies to
	// +optional
	PodSelector *metav1.LabelSelector `json:"podSelector,omitempty"`

	// Priority defines the policy priority (higher values take precedence)
	// +optional
	// +kubebuilder:default=0
	Priority int32 `json:"priority,omitempty"`

	// MIGConfig contains MIG-specific configuration
	// +optional
	MIGConfig *MIGConfig `json:"migConfig,omitempty"`

	// MPSConfig contains MPS-specific configuration
	// +optional
	MPSConfig *MPSConfig `json:"mpsConfig,omitempty"`

	// TimeSlicingConfig contains time-slicing configuration
	// +optional
	TimeSlicingConfig *TimeSlicingConfig `json:"timeSlicingConfig,omitempty"`
}

// MIGConfig contains MIG-specific configuration
type MIGConfig struct {
	// Profile specifies the MIG profile to use
	// Examples: "1g.5gb", "2g.10gb", "3g.20gb", "4g.20gb", "7g.40gb"
	// +optional
	Profile string `json:"profile,omitempty"`

	// AutoSelectProfile enables automatic profile selection
	// +optional
	// +kubebuilder:default=true
	AutoSelectProfile bool `json:"autoSelectProfile,omitempty"`
}

// MPSConfig contains MPS-specific configuration
type MPSConfig struct {
	// MaxClients specifies maximum number of MPS clients
	// +optional
	// +kubebuilder:default=16
	MaxClients int `json:"maxClients,omitempty"`

	// DefaultActiveThreads specifies default active thread percentage
	// +optional
	// +kubebuilder:default=100
	DefaultActiveThreads int `json:"defaultActiveThreads,omitempty"`

	// MemoryLimitMB specifies memory limit per client in MB
	// +optional
	MemoryLimitMB int64 `json:"memoryLimitMB,omitempty"`
}

// TimeSlicingConfig contains time-slicing configuration
type TimeSlicingConfig struct {
	// ReplicasPerGPU specifies number of virtual GPUs per physical GPU
	// +optional
	// +kubebuilder:default=4
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=16
	ReplicasPerGPU int `json:"replicasPerGPU,omitempty"`

	// SliceMs specifies time slice duration in milliseconds
	// +optional
	// +kubebuilder:default=100
	SliceMs int `json:"sliceMs,omitempty"`

	// FairnessMode specifies scheduling fairness mode
	// +optional
	// +kubebuilder:validation:Enum=roundrobin;priority;weighted
	// +kubebuilder:default=roundrobin
	FairnessMode string `json:"fairnessMode,omitempty"`
}

// GPUSharingPolicyStatus defines the observed state of GPUSharingPolicy
type GPUSharingPolicyStatus struct {
	// AppliedPods is the number of pods this policy has been applied to
	AppliedPods int32 `json:"appliedPods"`

	// LastUpdateTime is the timestamp of the last policy application
	// +optional
	LastUpdateTime *metav1.Time `json:"lastUpdateTime,omitempty"`

	// Conditions represent the latest available observations of the policy's state
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster,shortName=gsp
// +kubebuilder:printcolumn:name="Strategy",type=string,JSONPath=`.spec.strategy`
// +kubebuilder:printcolumn:name="Priority",type=integer,JSONPath=`.spec.priority`
// +kubebuilder:printcolumn:name="AppliedPods",type=integer,JSONPath=`.status.appliedPods`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// GPUSharingPolicy is the Schema for the gpusharingpolicies API
type GPUSharingPolicy struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   GPUSharingPolicySpec   `json:"spec,omitempty"`
	Status GPUSharingPolicyStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// GPUSharingPolicyList contains a list of GPUSharingPolicy
type GPUSharingPolicyList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []GPUSharingPolicy `json:"items"`
}

func init() {
	SchemeBuilder.Register(&GPUSharingPolicy{}, &GPUSharingPolicyList{})
}
