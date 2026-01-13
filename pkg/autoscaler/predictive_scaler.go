package autoscaler

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/gpuautoscaler/gpuautoscaler/pkg/metrics"
)

const (
	// Prediction configuration
	PredictionHorizon        = 30 * time.Minute // How far ahead to predict
	HistoricalLookback       = 7 * 24 * time.Hour // 7 days of historical data
	PatternSimilarityThreshold = 0.7 // Threshold for pattern matching
	PreWarmThreshold         = 0.7  // Utilization threshold to trigger pre-warming
)

// PredictiveScaler analyzes historical GPU utilization patterns and predicts future load
type PredictiveScaler struct {
	metricsCollector *metrics.Collector
	patterns         []UtilizationPattern
	lastUpdate       time.Time
}

// UtilizationPattern represents a historical utilization pattern
type UtilizationPattern struct {
	DayOfWeek    time.Weekday
	HourOfDay    int
	Duration     time.Duration
	AvgUtilization float64
	PeakUtilization float64
	PodsCount      int
	Trend          string // "increasing", "decreasing", "stable"
}

// ScalingPrediction contains prediction results
type ScalingPrediction struct {
	PredictedUtilization float64
	PredictedPods        int
	RecommendedNodes     int
	ShouldPreWarm        bool
	Confidence           float64
	Reason               string
	TimeUntilPeak        time.Duration
}

// NewPredictiveScaler creates a new predictive scaler
func NewPredictiveScaler(metricsCollector *metrics.Collector) *PredictiveScaler {
	return &PredictiveScaler{
		metricsCollector: metricsCollector,
		patterns:         make([]UtilizationPattern, 0),
	}
}

// PredictFutureLoad predicts future GPU load and makes scaling recommendations
func (p *PredictiveScaler) PredictFutureLoad(ctx context.Context) *ScalingPrediction {
	// Update patterns if needed
	if time.Since(p.lastUpdate) > time.Hour {
		if err := p.updatePatterns(ctx); err != nil {
			return &ScalingPrediction{
				ShouldPreWarm: false,
				Confidence:    0,
				Reason:        fmt.Sprintf("failed to update patterns: %v", err),
			}
		}
	}

	now := time.Now()
	prediction := &ScalingPrediction{
		ShouldPreWarm: false,
		Confidence:    0,
	}

	// Find similar historical patterns
	similarPatterns := p.findSimilarPatterns(now)
	if len(similarPatterns) == 0 {
		prediction.Reason = "no similar historical patterns found"
		return prediction
	}

	// Calculate weighted average prediction
	prediction.PredictedUtilization = p.calculateWeightedPrediction(similarPatterns)
	prediction.PredictedPods = p.predictPodCount(similarPatterns)
	prediction.Confidence = p.calculateConfidence(similarPatterns)

	// Determine if we should pre-warm nodes
	if prediction.PredictedUtilization > PreWarmThreshold && prediction.Confidence > 0.7 {
		prediction.ShouldPreWarm = true
		prediction.RecommendedNodes = p.calculateRecommendedNodes(prediction.PredictedUtilization, prediction.PredictedPods)
		prediction.TimeUntilPeak = p.estimateTimeUntilPeak(similarPatterns)
		prediction.Reason = fmt.Sprintf("predicted %.1f%% utilization in %s (confidence: %.1f%%)",
			prediction.PredictedUtilization*100,
			prediction.TimeUntilPeak.Round(time.Minute),
			prediction.Confidence*100,
		)
	} else {
		prediction.Reason = "no pre-warming needed"
	}

	return prediction
}

// updatePatterns updates historical patterns from metrics
func (p *PredictiveScaler) updatePatterns(ctx context.Context) error {
	// Query historical metrics
	endTime := time.Now()
	startTime := endTime.Add(-HistoricalLookback)

	patterns := make([]UtilizationPattern, 0)

	// Analyze patterns for each hour of each day of the week
	for day := time.Sunday; day <= time.Saturday; day++ {
		for hour := 0; hour < 24; hour++ {
			pattern := p.analyzePattern(ctx, day, hour, startTime, endTime)
			if pattern != nil {
				patterns = append(patterns, *pattern)
			}
		}
	}

	p.patterns = patterns
	p.lastUpdate = time.Now()

	return nil
}

// analyzePattern analyzes utilization for a specific day and hour
func (p *PredictiveScaler) analyzePattern(ctx context.Context, dayOfWeek time.Weekday, hourOfDay int, startTime, endTime time.Time) *UtilizationPattern {
	utilizationSamples := make([]float64, 0)
	podCounts := make([]int, 0)

	// Collect samples for this day/hour combination
	current := startTime
	for current.Before(endTime) {
		if current.Weekday() == dayOfWeek && current.Hour() == hourOfDay {
			// Get metrics for this hour
			gpuMetrics, err := p.metricsCollector.GetGPUMetrics(ctx)
			if err != nil {
				current = current.Add(24 * time.Hour)
				continue
			}

			if len(gpuMetrics) > 0 {
				var totalUtil float64
				podSet := make(map[string]bool)

				for _, metric := range gpuMetrics {
					totalUtil += metric.GPUUtilization
					podSet[metric.PodName] = true
				}

				avgUtil := totalUtil / float64(len(gpuMetrics))
				utilizationSamples = append(utilizationSamples, avgUtil)
				podCounts = append(podCounts, len(podSet))
			}
		}
		current = current.Add(24 * time.Hour)
	}

	if len(utilizationSamples) < 3 {
		// Not enough data
		return nil
	}

	// Calculate statistics
	avgUtil := p.average(utilizationSamples)
	peakUtil := p.max(utilizationSamples)
	avgPods := float64(p.sumInt(podCounts)) / float64(len(podCounts))
	trend := p.detectTrend(utilizationSamples)

	return &UtilizationPattern{
		DayOfWeek:      dayOfWeek,
		HourOfDay:      hourOfDay,
		Duration:       time.Hour,
		AvgUtilization: avgUtil,
		PeakUtilization: peakUtil,
		PodsCount:      int(avgPods),
		Trend:          trend,
	}
}

// findSimilarPatterns finds historical patterns similar to current time
func (p *PredictiveScaler) findSimilarPatterns(targetTime time.Time) []UtilizationPattern {
	targetDay := targetTime.Weekday()
	targetHour := targetTime.Hour()

	similar := make([]UtilizationPattern, 0)

	for _, pattern := range p.patterns {
		similarity := p.calculateSimilarity(pattern, targetDay, targetHour)
		if similarity > PatternSimilarityThreshold {
			similar = append(similar, pattern)
		}
	}

	return similar
}

// calculateSimilarity calculates similarity between a pattern and target time
func (p *PredictiveScaler) calculateSimilarity(pattern UtilizationPattern, targetDay time.Weekday, targetHour int) float64 {
	// Day similarity (same day = 1.0, adjacent day = 0.5, else = 0)
	daySimilarity := 0.0
	if pattern.DayOfWeek == targetDay {
		daySimilarity = 1.0
	} else if math.Abs(float64(pattern.DayOfWeek-targetDay)) == 1 {
		daySimilarity = 0.5
	}

	// Hour similarity (same hour = 1.0, ±1 hour = 0.7, ±2 hours = 0.4, else = 0)
	hourDiff := math.Abs(float64(pattern.HourOfDay - targetHour))
	hourSimilarity := 0.0
	if hourDiff == 0 {
		hourSimilarity = 1.0
	} else if hourDiff == 1 {
		hourSimilarity = 0.7
	} else if hourDiff == 2 {
		hourSimilarity = 0.4
	}

	// Combined similarity (weighted: 70% day, 30% hour)
	return daySimilarity*0.7 + hourSimilarity*0.3
}

// calculateWeightedPrediction calculates weighted average prediction from similar patterns
func (p *PredictiveScaler) calculateWeightedPrediction(patterns []UtilizationPattern) float64 {
	if len(patterns) == 0 {
		return 0
	}

	var totalWeight float64
	var weightedSum float64

	for _, pattern := range patterns {
		// Weight recent patterns more heavily
		weight := 1.0
		weightedSum += pattern.AvgUtilization * weight
		totalWeight += weight
	}

	return weightedSum / totalWeight
}

// predictPodCount predicts future pod count based on similar patterns
func (p *PredictiveScaler) predictPodCount(patterns []UtilizationPattern) int {
	if len(patterns) == 0 {
		return 0
	}

	var total int
	for _, pattern := range patterns {
		total += pattern.PodsCount
	}

	return total / len(patterns)
}

// calculateConfidence calculates prediction confidence based on pattern consistency
func (p *PredictiveScaler) calculateConfidence(patterns []UtilizationPattern) float64 {
	if len(patterns) < 2 {
		return 0.3 // Low confidence with limited data
	}

	// Calculate variance in predictions
	predictions := make([]float64, len(patterns))
	for i, pattern := range patterns {
		predictions[i] = pattern.AvgUtilization
	}

	variance := p.variance(predictions)
	stdDev := math.Sqrt(variance)

	// Lower variance = higher confidence
	// Map stdDev (0-1) to confidence (1-0)
	confidence := math.Max(0, 1.0-stdDev*2)

	// Boost confidence with more data points
	dataBoost := math.Min(0.2, float64(len(patterns))*0.05)
	confidence = math.Min(1.0, confidence+dataBoost)

	return confidence
}

// calculateRecommendedNodes calculates recommended node count based on prediction
func (p *PredictiveScaler) calculateRecommendedNodes(predictedUtilization float64, predictedPods int) int {
	// Estimate nodes needed based on predicted utilization and pod count
	// Assume each node can handle 8 GPUs at 80% utilization
	baseNodes := int(math.Ceil(float64(predictedPods) / 8.0))

	// Adjust for utilization intensity
	utilizationFactor := predictedUtilization / 0.8
	recommendedNodes := int(math.Ceil(float64(baseNodes) * utilizationFactor))

	return recommendedNodes
}

// estimateTimeUntilPeak estimates time until utilization peak
func (p *PredictiveScaler) estimateTimeUntilPeak(patterns []UtilizationPattern) time.Duration {
	if len(patterns) == 0 {
		return 0
	}

	// Find the pattern with highest utilization
	var peakPattern UtilizationPattern
	maxUtil := 0.0
	for _, pattern := range patterns {
		if pattern.PeakUtilization > maxUtil {
			maxUtil = pattern.PeakUtilization
			peakPattern = pattern
		}
	}

	// Calculate time until this pattern's hour
	now := time.Now()
	targetHour := peakPattern.HourOfDay

	hoursUntil := targetHour - now.Hour()
	if hoursUntil < 0 {
		hoursUntil += 24
	}

	return time.Duration(hoursUntil) * time.Hour
}

// detectTrend detects trend in a series of values
func (p *PredictiveScaler) detectTrend(values []float64) string {
	if len(values) < 3 {
		return "stable"
	}

	// Simple linear regression
	n := float64(len(values))
	var sumX, sumY, sumXY, sumX2 float64

	for i, y := range values {
		x := float64(i)
		sumX += x
		sumY += y
		sumXY += x * y
		sumX2 += x * x
	}

	slope := (n*sumXY - sumX*sumY) / (n*sumX2 - sumX*sumX)

	if slope > 0.05 {
		return "increasing"
	} else if slope < -0.05 {
		return "decreasing"
	}
	return "stable"
}

// AnalyzeBusyPeriods identifies recurring busy periods
func (p *PredictiveScaler) AnalyzeBusyPeriods() []BusyPeriod {
	busyPeriods := make([]BusyPeriod, 0)

	for _, pattern := range p.patterns {
		if pattern.AvgUtilization > 0.6 { // Consider 60%+ as busy
			busyPeriods = append(busyPeriods, BusyPeriod{
				DayOfWeek:  pattern.DayOfWeek,
				StartHour:  pattern.HourOfDay,
				Duration:   pattern.Duration,
				Utilization: pattern.AvgUtilization,
				Recurring:  true,
			})
		}
	}

	return busyPeriods
}

// BusyPeriod represents a recurring busy period
type BusyPeriod struct {
	DayOfWeek   time.Weekday
	StartHour   int
	Duration    time.Duration
	Utilization float64
	Recurring   bool
}

// GetPreWarmSchedule generates a schedule for pre-warming nodes
func (p *PredictiveScaler) GetPreWarmSchedule() []PreWarmEvent {
	busyPeriods := p.AnalyzeBusyPeriods()
	schedule := make([]PreWarmEvent, 0)

	for _, period := range busyPeriods {
		// Pre-warm 30 minutes before busy period
		preWarmTime := time.Duration(period.StartHour)*time.Hour - 30*time.Minute

		event := PreWarmEvent{
			DayOfWeek:    period.DayOfWeek,
			PreWarmTime:  preWarmTime,
			TargetNodes:  p.calculateRecommendedNodes(period.Utilization, 0),
			ExpectedLoad: period.Utilization,
		}
		schedule = append(schedule, event)
	}

	return schedule
}

// PreWarmEvent represents a scheduled pre-warming event
type PreWarmEvent struct {
	DayOfWeek    time.Weekday
	PreWarmTime  time.Duration
	TargetNodes  int
	ExpectedLoad float64
}

// Utility functions

func (p *PredictiveScaler) average(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	sum := 0.0
	for _, v := range values {
		sum += v
	}
	return sum / float64(len(values))
}

func (p *PredictiveScaler) max(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	max := values[0]
	for _, v := range values[1:] {
		if v > max {
			max = v
		}
	}
	return max
}

func (p *PredictiveScaler) variance(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	avg := p.average(values)
	var sumSquares float64
	for _, v := range values {
		diff := v - avg
		sumSquares += diff * diff
	}
	return sumSquares / float64(len(values))
}

func (p *PredictiveScaler) sumInt(values []int) int {
	sum := 0
	for _, v := range values {
		sum += v
	}
	return sum
}
