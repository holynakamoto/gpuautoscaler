package sharing

import (
	"testing"
)

func TestGetMIGProfile(t *testing.T) {
	manager := &MIGManager{}

	tests := []struct {
		name          string
		gpuRequest    int
		memoryRequest int64
		expectedProfile string
		shouldError   bool
	}{
		{
			name:            "Small workload - 1g.5gb",
			gpuRequest:      1,
			memoryRequest:   4 * 1024 * 1024 * 1024, // 4GB
			expectedProfile: "1g.5gb",
			shouldError:     false,
		},
		{
			name:            "Medium workload - 2g.10gb",
			gpuRequest:      1,
			memoryRequest:   8 * 1024 * 1024 * 1024, // 8GB
			expectedProfile: "2g.10gb",
			shouldError:     false,
		},
		{
			name:            "Large workload - 3g.20gb",
			gpuRequest:      2,
			memoryRequest:   18 * 1024 * 1024 * 1024, // 18GB
			expectedProfile: "3g.20gb",
			shouldError:     false,
		},
		{
			name:          "Very large workload - no suitable profile",
			gpuRequest:    8,
			memoryRequest: 100 * 1024 * 1024 * 1024, // 100GB
			shouldError:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			profile, err := manager.GetMIGProfile(tt.gpuRequest, tt.memoryRequest)

			if tt.shouldError {
				if err == nil {
					t.Error("Expected error, got nil")
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error: %v", err)
				}
				if profile.Name != tt.expectedProfile {
					t.Errorf("Expected profile %s, got %s", tt.expectedProfile, profile.Name)
				}
			}
		})
	}
}

func TestGetSupportedProfiles(t *testing.T) {
	profiles := GetSupportedProfiles()

	if len(profiles) == 0 {
		t.Error("Expected profiles, got empty list")
	}

	// Check for essential profiles
	profileNames := make(map[string]bool)
	for _, p := range profiles {
		profileNames[p.Name] = true
	}

	expectedProfiles := []string{"1g.5gb", "2g.10gb", "3g.20gb", "4g.20gb", "7g.40gb"}
	for _, expected := range expectedProfiles {
		if !profileNames[expected] {
			t.Errorf("Expected profile %s not found", expected)
		}
	}
}

func TestGetMIGDeviceResourceName(t *testing.T) {
	profile := MIGProfile{
		Name:     "1g.5gb",
		DeviceID: "1g.5gb",
	}

	resourceName := GetMIGDeviceResourceName(profile)
	expected := "nvidia.com/mig-1g.5gb"

	if resourceName != expected {
		t.Errorf("Expected %s, got %s", expected, resourceName)
	}
}

func TestMPSConfig(t *testing.T) {
	config := DefaultMPSConfig()

	if !config.Enabled {
		t.Error("Expected MPS to be enabled by default")
	}

	if config.MaxClients != 16 {
		t.Errorf("Expected 16 max clients, got %d", config.MaxClients)
	}

	if config.DefaultActiveThreads != 100 {
		t.Errorf("Expected 100 active threads, got %d", config.DefaultActiveThreads)
	}
}

func TestTimeSlicingConfig(t *testing.T) {
	config := DefaultTimeSlicingConfig()

	if !config.Enabled {
		t.Error("Expected time-slicing to be enabled by default")
	}

	if config.ReplicasPerGPU != 4 {
		t.Errorf("Expected 4 replicas per GPU, got %d", config.ReplicasPerGPU)
	}

	if config.DefaultSliceMs != 100 {
		t.Errorf("Expected 100ms slice, got %d", config.DefaultSliceMs)
	}

	if config.FairnessMode != "roundrobin" {
		t.Errorf("Expected roundrobin fairness, got %s", config.FairnessMode)
	}
}

func TestCalculateOptimalReplicas(t *testing.T) {
	tests := []struct {
		name             string
		avgUtilization   float64
		burstiness       float64
		expectedReplicas int
	}{
		{
			name:             "Very low utilization, high burstiness",
			avgUtilization:   15.0,
			burstiness:       0.8,
			expectedReplicas: 8,
		},
		{
			name:             "Low utilization, moderate burstiness",
			avgUtilization:   35.0,
			burstiness:       0.6,
			expectedReplicas: 4,
		},
		{
			name:             "Moderate utilization",
			avgUtilization:   55.0,
			burstiness:       0.4,
			expectedReplicas: 2,
		},
		{
			name:             "High utilization",
			avgUtilization:   75.0,
			burstiness:       0.2,
			expectedReplicas: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			replicas := CalculateOptimalReplicas(tt.avgUtilization, tt.burstiness)
			if replicas != tt.expectedReplicas {
				t.Errorf("Expected %d replicas, got %d", tt.expectedReplicas, replicas)
			}
		})
	}
}
