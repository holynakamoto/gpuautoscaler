package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// GPUNodeConfigSpec defines the desired GPU configuration for a node
type GPUNodeConfigSpec struct {
	// NodeName specifies the target node
	NodeName string `json:"nodeName"`

	// MIGEnabled enables MIG on the node
	// +optional
	MIGEnabled bool `json:"migEnabled,omitempty"`

	// MIGProfiles specifies the MIG profiles to configure
	// +optional
	MIGProfiles []string `json:"migProfiles,omitempty"`

	// MPSEnabled enables MPS on the node
	// +optional
	MPSEnabled bool `json:"mpsEnabled,omitempty"`

	// MPSMaxClients specifies maximum MPS clients
	// +optional
	// +kubebuilder:default=16
	MPSMaxClients int `json:"mpsMaxClients,omitempty"`

	// TimeSlicingEnabled enables time-slicing on the node
	// +optional
	TimeSlicingEnabled bool `json:"timeSlicingEnabled,omitempty"`

	// TimeSlicingReplicas specifies replicas per GPU
	// +optional
	// +kubebuilder:default=4
	TimeSlicingReplicas int `json:"timeSlicingReplicas,omitempty"`
}

// GPUNodeConfigStatus defines the observed state of GPUNodeConfig
type GPUNodeConfigStatus struct {
	// Phase represents the current phase of configuration
	// +kubebuilder:validation:Enum=Pending;Configuring;Ready;Failed
	Phase string `json:"phase"`

	// Message provides additional information about the configuration
	// +optional
	Message string `json:"message,omitempty"`

	// LastUpdateTime is the timestamp of the last configuration update
	// +optional
	LastUpdateTime *metav1.Time `json:"lastUpdateTime,omitempty"`

	// MIGStatus contains MIG configuration status
	// +optional
	MIGStatus *MIGStatus `json:"migStatus,omitempty"`

	// MPSStatus contains MPS configuration status
	// +optional
	MPSStatus *MPSStatusInfo `json:"mpsStatus,omitempty"`

	// TimeSlicingStatus contains time-slicing configuration status
	// +optional
	TimeSlicingStatus *TimeSlicingStatusInfo `json:"timeSlicingStatus,omitempty"`

	// Conditions represent the latest available observations
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// MIGStatus represents MIG configuration status
type MIGStatus struct {
	// Enabled indicates if MIG is enabled
	Enabled bool `json:"enabled"`

	// ConfiguredProfiles lists the configured MIG profiles
	// +optional
	ConfiguredProfiles []string `json:"configuredProfiles,omitempty"`

	// AvailableDevices lists available MIG devices
	// +optional
	AvailableDevices int `json:"availableDevices,omitempty"`
}

// MPSStatusInfo represents MPS configuration status
type MPSStatusInfo struct {
	// Enabled indicates if MPS is enabled
	Enabled bool `json:"enabled"`

	// ActiveClients is the number of active MPS clients
	// +optional
	ActiveClients int `json:"activeClients,omitempty"`

	// MaxClients is the configured maximum clients
	// +optional
	MaxClients int `json:"maxClients,omitempty"`
}

// TimeSlicingStatusInfo represents time-slicing configuration status
type TimeSlicingStatusInfo struct {
	// Enabled indicates if time-slicing is enabled
	Enabled bool `json:"enabled"`

	// PhysicalGPUs is the number of physical GPUs
	// +optional
	PhysicalGPUs int `json:"physicalGPUs,omitempty"`

	// VirtualGPUs is the number of virtual GPUs
	// +optional
	VirtualGPUs int `json:"virtualGPUs,omitempty"`

	// ReplicasPerGPU is the configured replicas
	// +optional
	ReplicasPerGPU int `json:"replicasPerGPU,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster,shortName=gnc
// +kubebuilder:printcolumn:name="Node",type=string,JSONPath=`.spec.nodeName`
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="MIG",type=boolean,JSONPath=`.spec.migEnabled`
// +kubebuilder:printcolumn:name="MPS",type=boolean,JSONPath=`.spec.mpsEnabled`
// +kubebuilder:printcolumn:name="TimeSlicing",type=boolean,JSONPath=`.spec.timeSlicingEnabled`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// GPUNodeConfig is the Schema for the gpunodeconfigs API
type GPUNodeConfig struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   GPUNodeConfigSpec   `json:"spec,omitempty"`
	Status GPUNodeConfigStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// GPUNodeConfigList contains a list of GPUNodeConfig
type GPUNodeConfigList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []GPUNodeConfig `json:"items"`
}

func init() {
	SchemeBuilder.Register(&GPUNodeConfig{}, &GPUNodeConfigList{})
}
