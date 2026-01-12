# Phase 4: Cost Management

## Overview

Phase 4 implements comprehensive GPU cost tracking, attribution, and budget management for Kubernetes clusters. It provides real-time cost visibility, spending controls, and ROI analysis to help teams optimize their GPU investments.

## Features

### 1. Real-time Cost Tracking 💵

Calculate GPU costs per second using cloud provider pricing APIs:

- **Multi-Cloud Pricing Integration**: AWS, GCP, Azure pricing APIs
- **Per-Second Granularity**: Track costs in real-time as pods run
- **Spot Price Tracking**: Monitor current spot instance pricing
- **GPU Type Pricing**: Accurate pricing for A100, H100, V100, T4, etc.
- **Sharing Cost Attribution**: Fair cost allocation for MIG/MPS/time-slicing

**How it works:**
- Cost tracker queries pod GPU usage every minute
- Fetches current pricing from cloud provider APIs
- Calculates incremental cost based on GPU type, capacity type (spot/on-demand), and sharing mode
- Persists cost data to TimescaleDB for historical analysis

### 2. Cost Attribution 📊

Track spending by namespace, label, experiment ID:

- **Namespace Attribution**: Track costs per Kubernetes namespace
- **Team Attribution**: Attribute costs to specific teams via labels
- **Experiment Tracking**: Link ML experiment IDs to costs
- **Project Tracking**: Group costs by project or workload
- **Cost Center Integration**: Map to financial cost centers
- **Custom Tags**: Flexible tagging for any attribution dimension

**Example: Track costs for ML experiments**
```yaml
apiVersion: gpu-autoscaler.io/v1alpha1
kind: CostAttribution
metadata:
  name: bert-training-costs
spec:
  experimentID: exp-bert-2024-001
  team: nlp-team
  project: language-models
  costCenter: research-ml
  retentionDays: 90
```

### 3. Budget Management 🎯

Set spending limits with alerts and enforcement:

- **Monthly Budgets**: Define spending limits per month
- **Multi-Threshold Alerts**: Notify at 50%, 80%, 100% thresholds
- **Multi-Channel Notifications**: Slack, Email, PagerDuty, Webhooks
- **Grace Periods**: Allow temporary budget overruns
- **Enforcement Actions**:
  - **Alert**: Notify only (default)
  - **Throttle**: Reduce spot instance usage
  - **Block**: Prevent new GPU pod creation
- **Budget Scoping**: Apply to namespaces, teams, or labels

**Example: Team budget with enforcement**
```yaml
apiVersion: gpu-autoscaler.io/v1alpha1
kind: CostBudget
metadata:
  name: data-science-budget
spec:
  monthlyLimit: 15000
  scope:
    teams:
      - data-science
  alerts:
    - name: budget-warning
      thresholdPercent: 80
      severity: warning
      channels:
        - type: slack
          config:
            webhook_url: "https://hooks.slack.com/..."
  enforcement:
    action: throttle
    gracePeriodMinutes: 60
    throttleConfig:
      maxSpotInstances: 5
```

### 4. ROI Reporting 📈

Demonstrate savings from GPU optimization:

- **Savings Breakdown**:
  - Spot instance savings (60-90% discount)
  - GPU sharing savings (MIG/MPS/time-slicing)
  - Autoscaling savings (dynamic resource adjustment)
  - Waste elimination (idle GPU detection)
- **ROI Metrics**:
  - Total savings vs. baseline costs
  - Savings percentage
  - Payback period for autoscaler investment
  - Projected annual savings
- **Cost Recommendations**: Actionable suggestions to reduce costs further

**CLI Usage:**
```bash
# Show cost summary
gpu-autoscaler cost

# Show detailed breakdown by namespace
gpu-autoscaler cost --format detailed

# Show ROI analysis
gpu-autoscaler cost --roi

# Filter by team
gpu-autoscaler cost --team data-science

# Show last 30 days
gpu-autoscaler cost --last 30d
```

## Architecture

### Components

1. **Cost Tracker** (`pkg/cost/tracker.go`)
   - Real-time pod cost calculation
   - Cloud pricing API integration
   - Per-second cost accumulation
   - Prometheus metrics export

2. **Pricing Client** (`pkg/cost/pricing.go`)
   - AWS, GCP, Azure pricing APIs
   - Spot price monitoring
   - Price caching (1-hour TTL)
   - Fallback to estimated pricing

3. **TimescaleDB Client** (`pkg/cost/timescaledb.go`)
   - Time-series cost data storage
   - Continuous aggregates for performance
   - Retention policies
   - Historical cost queries

4. **Attribution Controller** (`pkg/cost/attribution_controller.go`)
   - Reconciles CostAttribution resources
   - Aggregates costs by attribution criteria
   - Updates status with current spend
   - Calculates savings metrics

5. **Budget Controller** (`pkg/cost/budget_controller.go`)
   - Reconciles CostBudget resources
   - Monitors spending against limits
   - Triggers alerts at thresholds
   - Enforces budget policies

6. **Alert Manager** (`pkg/cost/alerter.go`)
   - Sends budget alerts
   - Supports Slack, Email, PagerDuty, Webhooks
   - Severity-based routing

7. **ROI Reporter** (`pkg/cost/roi_reporter.go`)
   - Calculates total savings
   - Generates ROI reports
   - Provides cost recommendations

## Installation

### Prerequisites

- GPU Autoscaler Phases 1-3 installed
- PostgreSQL with TimescaleDB extension
- Cloud provider credentials with pricing API access

### Enable Cost Tracking

```bash
# Install with cost tracking enabled
helm upgrade --install gpu-autoscaler charts/gpu-autoscaler \
  --set cost.enabled=true \
  --set cost.timescaledb.host=timescaledb.default.svc \
  --set cost.timescaledb.database=gpu_costs \
  --set cost.cloudProvider=aws \
  --set cost.region=us-east-1
```

### Configure TimescaleDB

```bash
# Deploy TimescaleDB
kubectl apply -f - <<EOF
apiVersion: apps/v1
kind: StatefulSet
metadata:
  name: timescaledb
  namespace: gpu-autoscaler
spec:
  serviceName: timescaledb
  replicas: 1
  selector:
    matchLabels:
      app: timescaledb
  template:
    metadata:
      labels:
        app: timescaledb
    spec:
      containers:
      - name: timescaledb
        image: timescale/timescaledb:latest-pg14
        env:
        - name: POSTGRES_DB
          value: gpu_costs
        - name: POSTGRES_PASSWORD
          valueFrom:
            secretKeyRef:
              name: timescaledb-secret
              key: password
        ports:
        - containerPort: 5432
        volumeMounts:
        - name: data
          mountPath: /var/lib/postgresql/data
  volumeClaimTemplates:
  - metadata:
      name: data
    spec:
      accessModes: ["ReadWriteOnce"]
      resources:
        requests:
          storage: 100Gi
---
apiVersion: v1
kind: Service
metadata:
  name: timescaledb
  namespace: gpu-autoscaler
spec:
  selector:
    app: timescaledb
  ports:
  - port: 5432
    targetPort: 5432
EOF
```

## Usage Examples

### 1. Track Costs by Namespace

```yaml
apiVersion: gpu-autoscaler.io/v1alpha1
kind: CostAttribution
metadata:
  name: ml-training-costs
spec:
  namespace: ml-training
  team: ml-engineering
  retentionDays: 90
```

```bash
# View costs
kubectl get costattribution ml-training-costs -o yaml

# Status shows:
status:
  totalCost: 15234.56
  dailyCost: 456.78
  monthlyCost: 13456.78
  hourlyCost: 18.99
  activePods: 12
  activeGPUs: 48
```

### 2. Set Team Budget with Alerts

```yaml
apiVersion: gpu-autoscaler.io/v1alpha1
kind: CostBudget
metadata:
  name: team-budget
spec:
  monthlyLimit: 10000
  scope:
    teams:
      - data-science
  alerts:
    - name: half-budget
      thresholdPercent: 50
      severity: info
      channels:
        - type: slack
          config:
            webhook_url: "https://hooks.slack.com/..."
    - name: budget-exceeded
      thresholdPercent: 100
      severity: critical
      channels:
        - type: pagerduty
          config:
            routing_key: "YOUR_KEY"
  enforcement:
    action: alert
```

### 3. Label Pods for Cost Tracking

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: training-job
  labels:
    team: nlp-team
    project: language-models
    experiment-id: exp-001
    cost-center: research
spec:
  containers:
  - name: trainer
    image: my-trainer:latest
    resources:
      requests:
        nvidia.com/gpu: "8"
```

### 4. View Cost Reports

```bash
# Summary view
gpu-autoscaler cost

# Detailed breakdown
gpu-autoscaler cost --format detailed

# Filter by namespace
gpu-autoscaler cost --namespace ml-training

# Show ROI analysis
gpu-autoscaler cost --roi

# Last 30 days
gpu-autoscaler cost --last 30d
```

## Cost Optimization Best Practices

### 1. Use Spot Instances (60-90% savings)
```yaml
apiVersion: gpu-autoscaler.io/v1alpha1
kind: AutoscalingPolicy
spec:
  nodePools:
    - name: training-spot
      capacityType: spot
      spotMaxPrice: 0.50
```

### 2. Enable GPU Sharing (50-75% savings for inference)
```yaml
apiVersion: gpu-autoscaler.io/v1alpha1
kind: GPUSharingPolicy
spec:
  strategy: mps  # or mig, timeslicing
  targetUtilization: 0.80
```

### 3. Set Pod Budgets
```yaml
metadata:
  annotations:
    gpu-autoscaler.io/cost-limit: "100"  # $100 maximum
```

### 4. Monitor Idle Resources
```bash
# Find underutilized GPUs
gpu-autoscaler optimize
```

### 5. Use Reserved Instances for Baseline Workloads
- Reserve 20-30% of capacity for predictable workloads
- Typical 30-50% savings vs. on-demand

## Metrics

Cost tracking exports Prometheus metrics:

- `gpu_autoscaler_total_cost_usd`: Total accumulated cost
- `gpu_autoscaler_hourly_cost_rate_usd`: Current hourly rate
- `gpu_autoscaler_pod_cost_usd{namespace, pod, gpu_type, capacity_type}`: Per-pod cost
- `gpu_autoscaler_total_savings_usd`: Total savings from optimizations
- `gpu_autoscaler_budget_percentage{budget}`: Budget utilization percentage

## Grafana Dashboards

Phase 4 includes pre-built Grafana dashboards:

- **Cost Overview**: Total spend, daily/monthly costs, budget status
- **Cost Attribution**: Breakdown by namespace, team, project
- **Savings Analysis**: Spot savings, sharing savings, ROI metrics
- **Budget Alerts**: Budget utilization, alert history

Import dashboards from `charts/gpu-autoscaler/dashboards/cost-management.json`

## Troubleshooting

### Cost Data Not Appearing

1. Check TimescaleDB connectivity:
```bash
kubectl logs -n gpu-autoscaler deployment/gpu-autoscaler-controller | grep timescale
```

2. Verify cloud provider credentials:
```bash
kubectl get secret -n gpu-autoscaler cloud-provider-creds
```

3. Check cost tracker is running:
```bash
kubectl logs -n gpu-autoscaler deployment/gpu-autoscaler-controller | grep "cost tracker"
```

### Budget Alerts Not Firing

1. Check alert configuration:
```bash
kubectl get costbudget <name> -o yaml
```

2. Verify webhook URLs are correct
3. Check controller logs for alert errors:
```bash
kubectl logs -n gpu-autoscaler deployment/gpu-autoscaler-controller | grep alert
```

### Incorrect Pricing

1. Verify cloud provider and region:
```bash
kubectl get configmap -n gpu-autoscaler gpu-autoscaler-config -o yaml
```

2. Check pricing cache:
```bash
# Pricing is cached for 1 hour
# Wait or restart controller to refresh
kubectl rollout restart -n gpu-autoscaler deployment/gpu-autoscaler-controller
```

## Integration with ML Platforms

### MLflow Integration

Track costs per MLflow experiment:

```python
import mlflow

# Start run with cost tracking labels
with mlflow.start_run():
    mlflow.set_tag("experiment-id", "exp-bert-001")
    mlflow.set_tag("team", "nlp-team")
    mlflow.set_tag("cost-center", "research")

    # Training code...
```

### Kubeflow Integration

Add cost labels to Kubeflow pipelines:

```python
from kfp import dsl

@dsl.pipeline(
    name="Training Pipeline",
    description="BERT training with cost tracking"
)
def training_pipeline():
    train_op = dsl.ContainerOp(
        name="train",
        image="my-trainer:latest",
    ).add_pod_label("team", "nlp-team") \
     .add_pod_label("experiment-id", "exp-001")
```

## API Reference

### CostAttribution

```yaml
apiVersion: gpu-autoscaler.io/v1alpha1
kind: CostAttribution
spec:
  namespace: string          # Kubernetes namespace
  team: string              # Team identifier
  project: string           # Project name
  experimentID: string      # ML experiment ID
  costCenter: string        # Financial cost center
  labels: map[string]string # Additional labels
  tags: map[string]string   # Custom tags
  retentionDays: int        # Data retention (1-365)

status:
  totalCost: float64
  dailyCost: float64
  monthlyCost: float64
  hourlyCost: float64
  activePods: int
  activeGPUs: int
  costPerGPUHour: float64
  detailedBreakdown: object
  savings: object
```

### CostBudget

```yaml
apiVersion: gpu-autoscaler.io/v1alpha1
kind: CostBudget
spec:
  monthlyLimit: float64
  scope:
    namespaces: []string
    labels: map[string]string
    teams: []string
    experimentID: string
  alerts: []Alert
  enforcement:
    action: string  # alert, throttle, block
    gracePeriodMinutes: int
    throttleConfig: object
  enabled: bool

status:
  currentSpend: float64
  percentageUsed: float64
  budgetStatus: string  # ok, warning, exceeded
  projectedMonthlySpend: float64
  daysRemaining: int
  alertsFired: []AlertFired
  enforcementActive: bool
  breakdown: object
```

## Next Steps

- **Phase 5**: Advanced scheduling with gang scheduling and multi-tenancy
- **Phase 6**: GPU health monitoring and predictive maintenance
- **Phase 7**: Cross-cluster GPU federation

## Support

For issues or questions:
- GitHub Issues: https://github.com/holynakamoto/gpuautoscaler/issues
- Documentation: https://github.com/holynakamoto/gpuautoscaler/docs
- Slack: #gpu-autoscaler
