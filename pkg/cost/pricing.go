package cost

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"sigs.k8s.io/controller-runtime/pkg/log"
)

// PricingClient fetches real-time GPU pricing from cloud providers
type PricingClient struct {
	provider      string // aws, gcp, azure
	region        string
	cacheEnabled  bool
	cacheDuration time.Duration

	// Price cache
	priceCache sync.Map // key -> *CachedPrice

	httpClient *http.Client
}

// GPUPricingRequest contains parameters for pricing lookup
type GPUPricingRequest struct {
	GPUType      string // e.g., "nvidia-tesla-a100", "nvidia-tesla-v100"
	CapacityType string // "spot", "on-demand", "reserved"
	Region       string
	Zone         string
}

// GPUPricing contains pricing information
type GPUPricing struct {
	GPUType          string
	CapacityType     string
	PricePerGPUHour  float64 // USD per GPU per hour
	PricePerGPUMonth float64 // USD per GPU per month (730 hours)
	Region           string
	Zone             string
	Currency         string
	LastUpdated      time.Time
}

// CachedPrice stores a price with expiration
type CachedPrice struct {
	Pricing   *GPUPricing
	ExpiresAt time.Time
}

// NewPricingClient creates a new pricing client
func NewPricingClient(provider, region string) *PricingClient {
	return &PricingClient{
		provider:      provider,
		region:        region,
		cacheEnabled:  true,
		cacheDuration: 1 * time.Hour, // Cache prices for 1 hour
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// GetGPUPricing retrieves pricing for a GPU configuration
func (pc *PricingClient) GetGPUPricing(ctx context.Context, req GPUPricingRequest) (*GPUPricing, error) {
	logger := log.FromContext(ctx)

	// Use region from request or fall back to client default
	region := req.Region
	if region == "" {
		region = pc.region
	}

	// Check cache first
	cacheKey := fmt.Sprintf("%s:%s:%s:%s", req.GPUType, req.CapacityType, region, req.Zone)
	if pc.cacheEnabled {
		if cached, ok := pc.priceCache.Load(cacheKey); ok {
			cachedPrice := cached.(*CachedPrice)
			if time.Now().Before(cachedPrice.ExpiresAt) {
				logger.V(2).Info("Using cached price", "key", cacheKey)
				return cachedPrice.Pricing, nil
			}
			// Expired - remove from cache
			pc.priceCache.Delete(cacheKey)
		}
	}

	// Fetch fresh pricing
	var pricing *GPUPricing
	var err error

	switch pc.provider {
	case "aws":
		pricing, err = pc.fetchAWSPricing(ctx, req, region)
	case "gcp":
		pricing, err = pc.fetchGCPPricing(ctx, req, region)
	case "azure":
		pricing, err = pc.fetchAzurePricing(ctx, req, region)
	default:
		// Fallback to estimated pricing
		pricing, err = pc.getEstimatedPricing(req, region)
	}

	if err != nil {
		logger.Error(err, "Failed to fetch pricing, using estimates", "provider", pc.provider)
		pricing, _ = pc.getEstimatedPricing(req, region)
	}

	// Cache the result
	if pc.cacheEnabled && pricing != nil {
		pc.priceCache.Store(cacheKey, &CachedPrice{
			Pricing:   pricing,
			ExpiresAt: time.Now().Add(pc.cacheDuration),
		})
	}

	return pricing, nil
}

// fetchAWSPricing gets pricing from AWS Pricing API
func (pc *PricingClient) fetchAWSPricing(ctx context.Context, req GPUPricingRequest, region string) (*GPUPricing, error) {
	logger := log.FromContext(ctx)

	// AWS Pricing API endpoint
	// Note: In production, use AWS SDK for more reliable access
	instanceType := mapGPUTypeToAWSInstance(req.GPUType)

	// For spot instances, use EC2 Spot Pricing API
	if req.CapacityType == "spot" {
		return pc.fetchAWSSpotPricing(ctx, instanceType, region)
	}

	// For on-demand, use Price List API
	// Simplified implementation - in production, use AWS Pricing SDK
	priceMap := getAWSOnDemandPrices()
	key := fmt.Sprintf("%s:%s", region, instanceType)

	if price, ok := priceMap[key]; ok {
		gpuCount := getGPUCountForInstanceType(instanceType)
		pricePerGPU := price / float64(gpuCount)

		return &GPUPricing{
			GPUType:          req.GPUType,
			CapacityType:     req.CapacityType,
			PricePerGPUHour:  pricePerGPU,
			PricePerGPUMonth: pricePerGPU * 730,
			Region:           region,
			Zone:             req.Zone,
			Currency:         "USD",
			LastUpdated:      time.Now(),
		}, nil
	}

	logger.V(1).Info("Price not found in map", "key", key)
	return pc.getEstimatedPricing(req, region)
}

// fetchAWSSpotPricing gets current spot prices
func (pc *PricingClient) fetchAWSSpotPricing(ctx context.Context, instanceType, region string) (*GPUPricing, error) {
	// In production, use AWS EC2 DescribeSpotPriceHistory API
	// Simplified: use recent average spot prices
	spotPrices := getAWSSpotPrices()
	key := fmt.Sprintf("%s:%s", region, instanceType)

	if price, ok := spotPrices[key]; ok {
		gpuCount := getGPUCountForInstanceType(instanceType)
		pricePerGPU := price / float64(gpuCount)

		return &GPUPricing{
			GPUType:          instanceType,
			CapacityType:     "spot",
			PricePerGPUHour:  pricePerGPU,
			PricePerGPUMonth: pricePerGPU * 730,
			Region:           region,
			Currency:         "USD",
			LastUpdated:      time.Now(),
		}, nil
	}

	return pc.getEstimatedPricing(GPUPricingRequest{
		GPUType:      instanceType,
		CapacityType: "spot",
		Region:       region,
	}, region)
}

// fetchGCPPricing gets pricing from GCP Cloud Billing API
func (pc *PricingClient) fetchGCPPricing(ctx context.Context, req GPUPricingRequest, region string) (*GPUPricing, error) {
	// In production, use GCP Cloud Billing API
	// https://cloud.google.com/billing/v1/how-tos/catalog-api

	machineType := mapGPUTypeToGCPMachine(req.GPUType)
	priceMap := getGCPPrices()

	capacityType := "on-demand"
	if req.CapacityType == "spot" {
		capacityType = "preemptible"
	}

	key := fmt.Sprintf("%s:%s:%s", region, machineType, capacityType)

	if price, ok := priceMap[key]; ok {
		gpuCount := getGPUCountForMachineType(machineType)
		pricePerGPU := price / float64(gpuCount)

		return &GPUPricing{
			GPUType:          req.GPUType,
			CapacityType:     req.CapacityType,
			PricePerGPUHour:  pricePerGPU,
			PricePerGPUMonth: pricePerGPU * 730,
			Region:           region,
			Currency:         "USD",
			LastUpdated:      time.Now(),
		}, nil
	}

	return pc.getEstimatedPricing(req, region)
}

// fetchAzurePricing gets pricing from Azure Retail Prices API
func (pc *PricingClient) fetchAzurePricing(ctx context.Context, req GPUPricingRequest, region string) (*GPUPricing, error) {
	// In production, use Azure Retail Prices API
	// https://learn.microsoft.com/en-us/rest/api/cost-management/retail-prices/azure-retail-prices

	vmSize := mapGPUTypeToAzureVM(req.GPUType)
	priceMap := getAzurePrices()

	capacityKey := "regular"
	if req.CapacityType == "spot" {
		capacityKey = "spot"
	}

	key := fmt.Sprintf("%s:%s:%s", region, vmSize, capacityKey)

	if price, ok := priceMap[key]; ok {
		gpuCount := getGPUCountForVMSize(vmSize)
		pricePerGPU := price / float64(gpuCount)

		return &GPUPricing{
			GPUType:          req.GPUType,
			CapacityType:     req.CapacityType,
			PricePerGPUHour:  pricePerGPU,
			PricePerGPUMonth: pricePerGPU * 730,
			Region:           region,
			Currency:         "USD",
			LastUpdated:      time.Now(),
		}, nil
	}

	return pc.getEstimatedPricing(req, region)
}

// getEstimatedPricing provides fallback pricing estimates
func (pc *PricingClient) getEstimatedPricing(req GPUPricingRequest, region string) (*GPUPricing, error) {
	// Estimated prices based on typical cloud pricing (as of 2024)
	estimates := map[string]float64{
		// NVIDIA A100
		"nvidia-tesla-a100":     3.00, // $3/hr on-demand
		"nvidia-a100-80gb":      4.00,
		// NVIDIA H100
		"nvidia-h100":           5.00,
		"nvidia-h100-80gb":      5.50,
		// NVIDIA V100
		"nvidia-tesla-v100":     2.50,
		"nvidia-tesla-v100-32gb": 2.80,
		// NVIDIA T4
		"nvidia-tesla-t4":       0.95,
		// NVIDIA A10
		"nvidia-a10":            1.20,
		// NVIDIA L4
		"nvidia-l4":             0.85,
	}

	pricePerGPU := 2.00 // default fallback
	if price, ok := estimates[req.GPUType]; ok {
		pricePerGPU = price
	}

	// Apply spot discount (typically 60-70% savings)
	if req.CapacityType == "spot" {
		pricePerGPU *= 0.35 // 65% discount
	}

	return &GPUPricing{
		GPUType:          req.GPUType,
		CapacityType:     req.CapacityType,
		PricePerGPUHour:  pricePerGPU,
		PricePerGPUMonth: pricePerGPU * 730,
		Region:           region,
		Currency:         "USD",
		LastUpdated:      time.Now(),
	}, nil
}

// Helper functions for price data
// In production, these would fetch from actual cloud APIs

func getAWSOnDemandPrices() map[string]float64 {
	return map[string]float64{
		// US East 1
		"us-east-1:p4d.24xlarge":  32.77, // 8x A100
		"us-east-1:p4de.24xlarge": 40.96, // 8x A100 80GB
		"us-east-1:p3.2xlarge":    3.06,  // 1x V100
		"us-east-1:p3.8xlarge":    12.24, // 4x V100
		"us-east-1:p3.16xlarge":   24.48, // 8x V100
		"us-east-1:g5.xlarge":     1.006, // 1x A10G
		"us-east-1:g5.2xlarge":    1.212, // 1x A10G
		"us-east-1:g4dn.xlarge":   0.526, // 1x T4
		"us-east-1:g4dn.2xlarge":  0.752, // 1x T4

		// US West 2
		"us-west-2:p4d.24xlarge":  32.77,
		"us-west-2:p3.2xlarge":    3.06,
		"us-west-2:g5.xlarge":     1.006,
		"us-west-2:g4dn.xlarge":   0.526,
	}
}

func getAWSSpotPrices() map[string]float64 {
	// Typical spot prices (30-40% of on-demand)
	return map[string]float64{
		"us-east-1:p4d.24xlarge":  11.47, // 8x A100 (35% of on-demand)
		"us-east-1:p4de.24xlarge": 14.34,
		"us-east-1:p3.2xlarge":    1.07,
		"us-east-1:p3.8xlarge":    4.28,
		"us-east-1:g5.xlarge":     0.35,
		"us-east-1:g4dn.xlarge":   0.18,
	}
}

func getGCPPrices() map[string]float64 {
	return map[string]float64{
		// US Central 1
		"us-central1:a2-highgpu-1g:on-demand":      3.67,  // 1x A100
		"us-central1:a2-highgpu-8g:on-demand":      29.39, // 8x A100
		"us-central1:a2-highgpu-1g:preemptible":    1.10,
		"us-central1:a2-highgpu-8g:preemptible":    8.82,
		"us-central1:n1-standard-4-t4:on-demand":   0.95,
		"us-central1:n1-standard-4-t4:preemptible": 0.29,

		// Europe West 4
		"europe-west4:a2-highgpu-1g:on-demand":      4.03,
		"europe-west4:a2-highgpu-8g:on-demand":      32.28,
		"europe-west4:a2-highgpu-1g:preemptible":    1.21,
		"europe-west4:a2-highgpu-8g:preemptible":    9.69,
	}
}

func getAzurePrices() map[string]float64 {
	return map[string]float64{
		// East US
		"eastus:Standard_ND96asr_v4:regular": 32.40, // 8x A100
		"eastus:Standard_ND96asr_v4:spot":    11.34,
		"eastus:Standard_NC6s_v3:regular":    3.06, // 1x V100
		"eastus:Standard_NC6s_v3:spot":       1.07,
		"eastus:Standard_NC4as_T4_v3:regular": 0.526,
		"eastus:Standard_NC4as_T4_v3:spot":    0.184,

		// West US 2
		"westus2:Standard_ND96asr_v4:regular": 32.40,
		"westus2:Standard_ND96asr_v4:spot":    11.34,
		"westus2:Standard_NC6s_v3:regular":    3.06,
		"westus2:Standard_NC6s_v3:spot":       1.07,
	}
}

func mapGPUTypeToAWSInstance(gpuType string) string {
	mapping := map[string]string{
		"nvidia-tesla-a100":   "p4d.24xlarge",
		"nvidia-a100-80gb":    "p4de.24xlarge",
		"nvidia-tesla-v100":   "p3.2xlarge",
		"nvidia-tesla-t4":     "g4dn.xlarge",
		"nvidia-a10":          "g5.xlarge",
	}
	if instance, ok := mapping[gpuType]; ok {
		return instance
	}
	return "p3.2xlarge" // default
}

func mapGPUTypeToGCPMachine(gpuType string) string {
	mapping := map[string]string{
		"nvidia-tesla-a100": "a2-highgpu-1g",
		"nvidia-tesla-v100": "n1-standard-4-v100",
		"nvidia-tesla-t4":   "n1-standard-4-t4",
	}
	if machine, ok := mapping[gpuType]; ok {
		return machine
	}
	return "a2-highgpu-1g" // default
}

func mapGPUTypeToAzureVM(gpuType string) string {
	mapping := map[string]string{
		"nvidia-tesla-a100": "Standard_ND96asr_v4",
		"nvidia-tesla-v100": "Standard_NC6s_v3",
		"nvidia-tesla-t4":   "Standard_NC4as_T4_v3",
	}
	if vm, ok := mapping[gpuType]; ok {
		return vm
	}
	return "Standard_ND96asr_v4" // default
}

func getGPUCountForInstanceType(instanceType string) int {
	counts := map[string]int{
		"p4d.24xlarge":   8,
		"p4de.24xlarge":  8,
		"p3.2xlarge":     1,
		"p3.8xlarge":     4,
		"p3.16xlarge":    8,
		"g5.xlarge":      1,
		"g5.2xlarge":     1,
		"g4dn.xlarge":    1,
		"g4dn.2xlarge":   1,
	}
	if count, ok := counts[instanceType]; ok {
		return count
	}
	return 1
}

func getGPUCountForMachineType(machineType string) int {
	counts := map[string]int{
		"a2-highgpu-1g": 1,
		"a2-highgpu-2g": 2,
		"a2-highgpu-4g": 4,
		"a2-highgpu-8g": 8,
	}
	if count, ok := counts[machineType]; ok {
		return count
	}
	return 1
}

func getGPUCountForVMSize(vmSize string) int {
	counts := map[string]int{
		"Standard_ND96asr_v4":   8,
		"Standard_ND96amsr_A100_v4": 8,
		"Standard_NC6s_v3":      1,
		"Standard_NC12s_v3":     2,
		"Standard_NC24s_v3":     4,
		"Standard_NC4as_T4_v3":  1,
	}
	if count, ok := counts[vmSize]; ok {
		return count
	}
	return 1
}

// ClearCache removes all cached prices
func (pc *PricingClient) ClearCache() {
	pc.priceCache = sync.Map{}
}
