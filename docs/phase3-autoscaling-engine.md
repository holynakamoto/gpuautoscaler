# Phase 3: Autoscaling Engine

## Overview

Phase 3 introduces a comprehensive GPU-aware autoscaling engine that automatically scales your GPU cluster based on actual GPU utilization, not just CPU or memory metrics. The autoscaler includes:

- **🚀 GPU-Aware Autoscaling**: Scale based on GPU utilization and pending GPU pod queue
- **💸 Spot Instance Orchestration**: Prioritize cheaper spot instances with graceful eviction handling
- **🎚️ Multi-Tier Scaling**: Optimize costs with spot → on-demand → reserved instance strategy
- **🔮 Predictive Scaling**: Pre-warm nodes for known busy periods based on historical patterns

## Key Features

### 1. GPU-Aware Scaling

Unlike traditional Kubernetes autoscalers that only consider CPU/memory, our autoscaler scales based on:

- **GPU Utilization**: Scale up when cluster GPU utilization exceeds 80%
- **Pending GPU Pods**: Scale up when GPU pods are pending for >2 minutes
- **Underutilization**: Scale down when nodes have <20% GPU utilization for >10 minutes

### 2. Spot Instance Orchestration

Reduce costs by 60-90% with intelligent spot instance management:

- **Automatic Spot Termination Handling**: Monitors cloud provider APIs for termination notices
- **Graceful Pod Eviction**: Prioritizes workload eviction to minimize disruption
  - Low priority: Development/batch workloads (evicted first)
  - Medium priority: Inference workloads
  - High priority: Training workloads (evicted last, given more time)
- **Spot Interruption Monitoring**: Tracks interruption rates and adjusts strategy
- **Multi-Instance Type Diversification**: Spreads workloads across instance types to reduce risk

### 3. Multi-Tier Scaling Strategy

Optimize cost vs. reliability with a three-tier approach:

1. **Spot Instances (60%)**: Cheapest option, 60-90% savings
   - Primary choice for new nodes
   - Automatic failover on interruption

2. **On-Demand Instances (35%)**: Reliable, no interruptions
   - Fallback when spot unavailable
   - Critical workloads

3. **Reserved Instances (5%)**: Maximum savings for baseline
   - Long-term commitments
   - Always-on workloads

### 4. Predictive Scaling

Pre-warm nodes before demand spikes:

- **Historical Pattern Analysis**: Learns from 7 days of GPU utilization data
- **Time-Based Patterns**: Identifies recurring busy periods (day/hour)
- **Confidence Scoring**: Only acts on high-confidence predictions (>70%)
- **Pre-Warming**: Adds nodes 30 minutes before predicted peak

**Example**: If your team trains models every weekday at 9 AM, the autoscaler will pre-warm nodes at 8:30 AM.

## Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                 Autoscaling Controller                       │
├─────────────────────────────────────────────────────────────┤
│                                                              │
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐     │
│  │   GPU-Aware  │  │    Spot      │  │  Predictive  │     │
│  │   Scaling    │  │ Orchestrator │  │   Scaler     │     │
│  └──────┬───────┘  └──────┬───────┘  └──────┬───────┘     │
│         │                  │                  │              │
│         └──────────────────┼──────────────────┘             │
│                            │                                 │
└────────────────────────────┼─────────────────────────────────┘
                             │
                             ↓
              ┌──────────────────────────┐
              │   Cloud Provider API     │
              │  (AWS / GCP / Azure)     │
              └──────────────────────────┘
                             │
                             ↓
              ┌──────────────────────────┐
              │   Auto Scaling Groups    │
              │   / Instance Groups      │
              │   / VM Scale Sets        │
              └──────────────────────────┘
```

## Quick Start

### 1. Enable Autoscaling

Update your Helm values:

```yaml
autoscaling:
  enabled: true
  provider: aws  # or gcp, azure

  scaleUp:
    threshold: 0.8  # 80% GPU utilization
    cooldown: 3m
    pendingPodTimeout: 2m

  scaleDown:
    threshold: 0.2  # 20% GPU utilization
    cooldown: 10m

  limits:
    minNodes: 0
    maxNodes: 100

  spot:
    enabled: true
    targetPercentage: 0.6  # 60% spot instances
    gracefulEviction: true

  multiTier:
    enabled: true
    spotPercentage: 0.6
    onDemandPercentage: 0.35
    reservedPercentage: 0.05
```

### 2. Configure Node Pools

Define your GPU node pools:

```yaml
autoscaling:
  nodePools:
    - name: spot-pool
      minSize: 0
      maxSize: 50
      gpuType: nvidia-tesla-v100
      instanceTypes:
        - p3.2xlarge
        - p3.8xlarge
      capacityType: spot
      priority: 10

    - name: on-demand-pool
      minSize: 0
      maxSize: 30
      gpuType: nvidia-tesla-v100
      instanceTypes:
        - p3.2xlarge
      capacityType: on-demand
      priority: 5
```

### 3. Deploy

```bash
helm upgrade --install gpu-autoscaler ./charts/gpu-autoscaler \
  --namespace gpu-autoscaler-system \
  --create-namespace \
  -f custom-values.yaml
```

## Configuration Reference

### AutoscalingPolicy CRD

You can also configure autoscaling via the `AutoscalingPolicy` CRD:

```yaml
apiVersion: gpu-autoscaler.io/v1alpha1
kind: AutoscalingPolicy
metadata:
  name: production-autoscaling
spec:
  enabled: true
  provider: aws

  scaleUpThreshold: 0.8
  scaleDownThreshold: 0.2
  scaleUpCooldownSeconds: 180
  scaleDownCooldownSeconds: 600
  pendingPodTimeoutSeconds: 120

  minNodes: 0
  maxNodes: 100

  spotInstancePercentage: 0.6
  enableSpotInstances: true
  enableMultiTierScaling: true
  enablePredictiveScaling: false

  nodePools:
    - name: spot-pool
      minSize: 0
      maxSize: 50
      gpuType: nvidia-tesla-v100
      instanceTypes:
        - p3.2xlarge
        - p3.8xlarge
      capacityType: spot
      spotPercentage: 1.0
      priority: 10
      labels:
        gpu-autoscaler.io/capacity-type: spot
      availabilityZones:
        - us-west-2a
        - us-west-2b
        - us-west-2c
```

Check status:

```bash
kubectl get autoscalingpolicy production-autoscaling -o yaml
```

## Monitoring

### Prometheus Metrics

Phase 3 exports comprehensive metrics:

**Scaling Actions:**
- `gpu_autoscaler_scaling_actions_total`: Total scaling actions (scale-up, scale-down)
- `gpu_autoscaler_scaling_duration_seconds`: Time to complete scaling

**Node Metrics:**
- `gpu_autoscaler_node_count`: Current node count by capacity type
- `gpu_autoscaler_desired_node_count`: Target node count

**Utilization:**
- `gpu_autoscaler_cluster_utilization`: Average GPU utilization (0-1)
- `gpu_autoscaler_pending_pods`: Number of pending GPU pods
- `gpu_autoscaler_underutilized_nodes`: Underutilized node count

**Spot Instances:**
- `gpu_autoscaler_spot_interruptions_total`: Spot interruption count
- `gpu_autoscaler_spot_termination_warnings`: Active termination warnings
- `gpu_autoscaler_spot_savings_percentage`: Estimated savings from spot

**Cost:**
- `gpu_autoscaler_estimated_monthly_cost_usd`: Estimated cost by capacity type
- `gpu_autoscaler_estimated_monthly_savings_usd`: Total estimated savings

**Predictive Scaling:**
- `gpu_autoscaler_predicted_utilization`: Predicted GPU utilization
- `gpu_autoscaler_prediction_confidence`: Confidence level (0-1)

### Grafana Dashboard

Import the Phase 3 dashboard for visualization:

```bash
kubectl apply -f examples/phase3/grafana-dashboard.yaml
```

Dashboard includes:
- Current vs. desired node count
- GPU utilization trends
- Spot interruption rate
- Cost breakdown by capacity type
- Scaling action history
- Predictive scaling recommendations

## Cloud Provider Setup

### AWS

1. **Create Auto Scaling Groups**:

```bash
# Spot ASG
aws autoscaling create-auto-scaling-group \
  --auto-scaling-group-name gpu-spot-asg \
  --launch-template LaunchTemplateName=gpu-spot-template \
  --min-size 0 \
  --max-size 50 \
  --vpc-zone-identifier subnet-xxx,subnet-yyy

# On-demand ASG
aws autoscaling create-auto-scaling-group \
  --auto-scaling-group-name gpu-on-demand-asg \
  --launch-template LaunchTemplateName=gpu-on-demand-template \
  --min-size 0 \
  --max-size 30
```

2. **Configure IAM Permissions**:

```json
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Effect": "Allow",
      "Action": [
        "autoscaling:DescribeAutoScalingGroups",
        "autoscaling:SetDesiredCapacity",
        "autoscaling:TerminateInstanceInAutoScalingGroup",
        "ec2:DescribeInstances",
        "ec2:DescribeSpotPriceHistory"
      ],
      "Resource": "*"
    }
  ]
}
```

3. **Update Helm values**:

```yaml
autoscaling:
  provider: aws
  aws:
    region: us-west-2
    autoScalingGroups:
      spot-pool: gpu-spot-asg
      on-demand-pool: gpu-on-demand-asg
```

### GCP

1. **Create Managed Instance Groups**:

```bash
# Spot MIG
gcloud compute instance-groups managed create gpu-spot-mig \
  --size 0 \
  --template gpu-spot-template \
  --zone us-central1-a

gcloud compute instance-groups managed set-autoscaling gpu-spot-mig \
  --max-num-replicas 50 \
  --zone us-central1-a
```

2. **Configure IAM Permissions**:

```yaml
roles:
  - compute.instanceAdmin.v1
  - compute.viewer
```

3. **Update Helm values**:

```yaml
autoscaling:
  provider: gcp
  gcp:
    projectID: my-project
    region: us-central1
    instanceGroups:
      spot-pool: gpu-spot-mig
      on-demand-pool: gpu-on-demand-mig
```

### Azure

1. **Create VM Scale Sets**:

```bash
az vmss create \
  --name gpu-spot-vmss \
  --resource-group gpu-cluster \
  --image gpu-ubuntu-image \
  --priority Spot \
  --eviction-policy Delete \
  --instance-count 0
```

2. **Configure RBAC**:

```bash
az role assignment create \
  --role "Virtual Machine Contributor" \
  --assignee <service-principal-id>
```

3. **Update Helm values**:

```yaml
autoscaling:
  provider: azure
  azure:
    subscriptionID: xxx
    resourceGroup: gpu-cluster
    region: eastus
    vmScaleSets:
      spot-pool: gpu-spot-vmss
      on-demand-pool: gpu-on-demand-vmss
```

## Advanced Configuration

### Predictive Scaling

Enable predictive scaling to pre-warm nodes:

```yaml
autoscaling:
  predictive:
    enabled: true
    lookbackDays: 7
    horizonMinutes: 30
    confidenceThreshold: 0.7
    preWarmThreshold: 0.7
```

**How it works**:
1. Analyzes 7 days of GPU utilization history
2. Identifies patterns by day of week and hour
3. Predicts utilization 30 minutes ahead
4. Pre-warms nodes if confidence >70% and predicted utilization >70%

### Custom Eviction Priorities

Control which pods are evicted first during spot termination:

```yaml
apiVersion: v1
kind: Pod
metadata:
  annotations:
    gpu-autoscaler.io/eviction-priority: high  # low, medium, high
spec:
  ...
```

**Priority levels**:
- **Low**: Development, batch jobs (evicted first)
- **Medium**: Inference, serving (evicted second)
- **High**: Training, critical workloads (evicted last)

### Spot Termination Handling

The autoscaler automatically handles spot interruptions:

1. **Detection**: Polls cloud metadata every 5 seconds
2. **Warning**: Receives 30s-2min notice (varies by cloud)
3. **Cordon**: Marks node as unschedulable
4. **Eviction**: Drains pods in priority order
5. **Replacement**: Launches replacement on alternative node

## Cost Analysis

### Expected Savings

**Spot Instances**:
- AWS: 60-90% savings vs. on-demand
- GCP: 60-91% savings vs. regular
- Azure: 60-90% savings vs. pay-as-you-go

**Example Calculation** (AWS p3.2xlarge):
- On-demand: $3.06/hour = $2,203/month
- Spot: ~$1.20/hour = $864/month
- **Savings: $1,339/month per node (61%)**

**With 10 nodes at 60% spot**:
- 6 spot nodes: 6 × $864 = $5,184/month
- 4 on-demand nodes: 4 × $2,203 = $8,812/month
- **Total: $13,996/month**
- **vs. All on-demand: $22,030/month**
- **Savings: $8,034/month (36%)**

### Cost Monitoring

Query Prometheus for cost metrics:

```promql
# Current monthly cost estimate
sum(gpu_autoscaler_estimated_monthly_cost_usd)

# Savings from spot instances
gpu_autoscaler_estimated_monthly_savings_usd

# Cost breakdown by capacity type
sum by (capacity_type) (gpu_autoscaler_estimated_monthly_cost_usd)
```

## Troubleshooting

### Autoscaler Not Scaling

**Check controller logs**:
```bash
kubectl logs -n gpu-autoscaler-system deployment/gpu-autoscaler-controller
```

**Common issues**:
1. **Cooldown period**: Wait for cooldown to expire
2. **Max nodes reached**: Increase `maxNodes` limit
3. **Cloud provider permissions**: Verify IAM/RBAC
4. **Node pool configuration**: Check ASG/MIG/VMSS names

**Check metrics**:
```bash
# Cooldown remaining
gpu_autoscaler_scale_up_cooldown_remaining_seconds
gpu_autoscaler_scale_down_cooldown_remaining_seconds

# Reconcile errors
gpu_autoscaler_reconcile_errors_total
```

### Spot Interruptions Too Frequent

**Increase diversification**:
```yaml
autoscaling:
  spot:
    diversify: true
  nodePools:
    - name: spot-pool
      instanceTypes:
        - p3.2xlarge
        - p3.8xlarge
        - g4dn.xlarge  # Add more types
        - g5.xlarge
```

**Adjust spot percentage**:
```yaml
autoscaling:
  spot:
    targetPercentage: 0.4  # Reduce from 0.6
```

### Predictive Scaling Inaccurate

**Increase confidence threshold**:
```yaml
autoscaling:
  predictive:
    confidenceThreshold: 0.8  # Increase from 0.7
```

**Increase lookback period**:
```yaml
autoscaling:
  predictive:
    lookbackDays: 14  # Increase from 7
```

## Best Practices

### 1. Start Conservative

Begin with conservative settings and tune over time:

```yaml
autoscaling:
  scaleUp:
    threshold: 0.7  # Start lower
  spot:
    targetPercentage: 0.4  # Start with less spot
  predictive:
    enabled: false  # Enable after observing patterns
```

### 2. Use Pod Disruption Budgets

Protect critical workloads:

```yaml
apiVersion: policy/v1
kind: PodDisruptionBudget
metadata:
  name: training-pdb
spec:
  minAvailable: 2
  selector:
    matchLabels:
      workload-type: training
```

### 3. Label Workloads

Help the autoscaler make better decisions:

```yaml
metadata:
  labels:
    gpu-autoscaler.io/workload-type: training
  annotations:
    gpu-autoscaler.io/eviction-priority: high
```

### 4. Monitor Interruption Rates

Track spot interruptions and adjust strategy:

```promql
rate(gpu_autoscaler_spot_interruptions_total[24h])
```

If rate >10%, consider:
- Reducing spot percentage
- Increasing instance type diversity
- Using different availability zones

### 5. Test Eviction Handling

Simulate spot termination to verify graceful handling:

```bash
# Manually mark node for termination
kubectl annotate node <node-name> \
  gpu-autoscaler.io/spot-interruption-warning=true \
  gpu-autoscaler.io/spot-termination-time=$(date -u +"%Y-%m-%dT%H:%M:%SZ" -d "+2 minutes")
```

## Roadmap

### Phase 3.1 (Coming Soon)
- Gang scheduling support for distributed training
- Custom metrics support (scale on custom metrics)
- Cost-based optimization (automatically choose cheapest instance types)
- Integration with Karpenter for node provisioning

### Phase 3.2 (Future)
- ML-powered predictive scaling (LSTM models)
- Cross-region autoscaling
- Spot bid optimization
- Real-time cost dashboards

## Support

- Documentation: https://github.com/holynakamoto/gpuautoscaler/docs
- Issues: https://github.com/holynakamoto/gpuautoscaler/issues
- Discussions: https://github.com/holynakamoto/gpuautoscaler/discussions

## License

Apache License 2.0
