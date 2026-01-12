package scheduler

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestBestFitNode(t *testing.T) {
	scheduler := &BinPackingScheduler{
		packStrategy: BestFit,
	}

	nodes := []*GPUNode{
		{Name: "node1", TotalGPUs: 8, AvailableGPUs: 6},
		{Name: "node2", TotalGPUs: 8, AvailableGPUs: 3},
		{Name: "node3", TotalGPUs: 8, AvailableGPUs: 8},
	}

	workload := &GPUWorkload{
		GPURequest: 2,
	}

	// Best fit should select node2 (waste = 3 - 2 = 1, smallest waste)
	selected := scheduler.bestFitNode(nodes, workload)
	if selected.Name != "node2" {
		t.Errorf("Expected node2, got %s", selected.Name)
	}
}

func TestWorstFitNode(t *testing.T) {
	scheduler := &BinPackingScheduler{
		packStrategy: WorstFit,
	}

	nodes := []*GPUNode{
		{Name: "node1", TotalGPUs: 8, AvailableGPUs: 6},
		{Name: "node2", TotalGPUs: 8, AvailableGPUs: 3},
		{Name: "node3", TotalGPUs: 8, AvailableGPUs: 8},
	}

	// Worst fit should select node3 (most available GPUs)
	selected := scheduler.worstFitNode(nodes)
	if selected.Name != "node3" {
		t.Errorf("Expected node3, got %s", selected.Name)
	}
}

func TestCalculateFragmentation(t *testing.T) {
	nodes := []*GPUNode{
		{Name: "node1", TotalGPUs: 8, AvailableGPUs: 2},
		{Name: "node2", TotalGPUs: 8, AvailableGPUs: 2},
		{Name: "node3", TotalGPUs: 8, AvailableGPUs: 0},
	}

	fragmentation := CalculateFragmentation(nodes)
	if fragmentation < 0 || fragmentation > 1 {
		t.Errorf("Fragmentation score should be between 0 and 1, got %.2f", fragmentation)
	}
}

func TestGetGPURequestFromPod(t *testing.T) {
	pod := &corev1.Pod{
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							"nvidia.com/gpu": resource.MustParse("2"),
						},
					},
				},
				{
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							"nvidia.com/gpu": resource.MustParse("1"),
						},
					},
				},
			},
		},
	}

	gpuCount := GetGPURequestFromPod(pod)
	if gpuCount != 3 {
		t.Errorf("Expected 3 GPUs, got %d", gpuCount)
	}
}

func TestSelectNode(t *testing.T) {
	scheduler := &BinPackingScheduler{
		packStrategy: BestFit,
	}

	nodes := []*GPUNode{
		{Name: "node1", TotalGPUs: 8, AvailableGPUs: 4},
		{Name: "node2", TotalGPUs: 8, AvailableGPUs: 2},
		{Name: "node3", TotalGPUs: 8, AvailableGPUs: 1},
	}

	tests := []struct {
		name           string
		gpuRequest     int
		expectedNode   string
		shouldBeNil    bool
	}{
		{
			name:         "Request fits in smallest node",
			gpuRequest:   1,
			expectedNode: "node3",
			shouldBeNil:  false,
		},
		{
			name:         "Request fits in medium node",
			gpuRequest:   2,
			expectedNode: "node2",
			shouldBeNil:  false,
		},
		{
			name:        "Request too large",
			gpuRequest:  5,
			shouldBeNil: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			workload := &GPUWorkload{
				GPURequest: tt.gpuRequest,
				Pod: &corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-pod",
					},
				},
			}

			selected := scheduler.selectNode(nodes, workload)
			if tt.shouldBeNil {
				if selected != nil {
					t.Errorf("Expected nil, got %s", selected.Name)
				}
			} else {
				if selected == nil {
					t.Error("Expected node, got nil")
				} else if selected.Name != tt.expectedNode {
					t.Errorf("Expected %s, got %s", tt.expectedNode, selected.Name)
				}
			}
		})
	}
}
