# Phase 3 Examples: Autoscaling Engine

This directory contains example configurations for Phase 3: Autoscaling Engine.

## Quick Start

### 1. AWS Example

Deploy autoscaling with spot instances on AWS:

```bash
kubectl apply -f autoscaling-policy-aws.yaml
```

This creates three node pools:
- **Spot pool**: 60% of workload, 60-90% cost savings
- **On-demand pool**: 35% of workload, reliable
- **Reserved pool**: 5% baseline (if you have reserved instances)

### 2. GCP Example

Deploy autoscaling with preemptible instances on GCP:

```bash
kubectl apply -f autoscaling-policy-gcp.yaml
```

### 3. Azure Example

Deploy autoscaling with spot instances on Azure:

```bash
kubectl apply -f autoscaling-policy-azure.yaml
```

## Configuration Examples

### Basic Autoscaling

Minimal configuration to get started:

```yaml
apiVersion: gpu-autoscaler.io/v1alpha1
kind: AutoscalingPolicy
metadata:
  name: simple-autoscaling
spec:
  enabled: true
  provider: aws

  scaleUpThreshold: 0.8
  scaleDownThreshold: 0.2

  minNodes: 0
  maxNodes: 50

  nodePools:
    - name: default-pool
      minSize: 0
      maxSize: 50
      instanceTypes:
        - p3.2xlarge
      capacityType: on-demand
```

### With Spot Instances

Add spot instances for cost savings:

```yaml
spec:
  spotInstancePercentage: 0.6  # 60% spot
  enableSpotInstances: true

  nodePools:
    - name: spot-pool
      instanceTypes:
        - p3.2xlarge
        - p3.8xlarge
      capacityType: spot
      spotPercentage: 1.0
      priority: 10

    - name: on-demand-pool
      instanceTypes:
        - p3.2xlarge
      capacityType: on-demand
      priority: 5
```

### With Predictive Scaling

Enable predictive scaling for known busy periods:

```yaml
spec:
  enablePredictiveScaling: true

  # Analyze 7 days of history
  # Predict 30 minutes ahead
  # Pre-warm nodes with >70% confidence
```

### Conservative Production

More conservative settings for production:

```yaml
spec:
  scaleUpThreshold: 0.85       # Higher threshold
  scaleDownThreshold: 0.15     # Lower threshold
  scaleUpCooldownSeconds: 300  # Longer cooldown
  scaleDownCooldownSeconds: 900

  minNodes: 5  # Always keep 5 nodes
  spotInstancePercentage: 0.3  # Less spot usage
```

### Aggressive Development

Aggressive settings for dev/test:

```yaml
spec:
  scaleUpThreshold: 0.7
  scaleDownThreshold: 0.3
  scaleUpCooldownSeconds: 60
  scaleDownCooldownSeconds: 300

  minNodes: 0  # Scale to zero
  spotInstancePercentage: 0.8  # 80% spot
```

## Monitoring

### Check Autoscaling Status

```bash
# View policy status
kubectl get autoscalingpolicy -o wide

# Detailed status
kubectl describe autoscalingpolicy production-aws-autoscaling

# Watch scaling events
kubectl get events --field-selector involvedObject.kind=AutoscalingPolicy
```

### Prometheus Queries

```promql
# Current node count
gpu_autoscaler_node_count

# GPU utilization
gpu_autoscaler_cluster_utilization

# Pending pods
gpu_autoscaler_pending_pods

# Scaling actions
rate(gpu_autoscaler_scaling_actions_total[1h])

# Spot interruptions
gpu_autoscaler_spot_interruptions_total

# Monthly cost estimate
sum(gpu_autoscaler_estimated_monthly_cost_usd)
```

## Testing

### Test Scale-Up

Create pending GPU pods:

```bash
kubectl apply -f - <<EOF
apiVersion: v1
kind: Pod
metadata:
  name: test-gpu-pod
spec:
  containers:
  - name: cuda
    image: nvidia/cuda:11.8.0-base-ubuntu22.04
    command: ["sleep", "3600"]
    resources:
      limits:
        nvidia.com/gpu: 1
EOF
```

Watch autoscaler scale up:
```bash
kubectl get nodes -w
```

### Test Scale-Down

Delete pods and watch scale-down after cooldown:

```bash
kubectl delete pod test-gpu-pod
# Wait 10 minutes (default cooldown)
kubectl get nodes -w
```

### Test Spot Termination

Simulate spot termination notice:

```bash
# Mark node for termination
kubectl annotate node <node-name> \
  gpu-autoscaler.io/spot-interruption-warning=true \
  gpu-autoscaler.io/spot-termination-time=$(date -u +"%Y-%m-%dT%H:%M:%SZ" -d "+2 minutes")

# Watch pod eviction
kubectl get pods -w
```

## Cloud Provider Setup

### AWS

1. **Create IAM Role**:
```bash
aws iam create-role --role-name GPUAutoscalerRole \
  --assume-role-policy-document file://trust-policy.json

aws iam put-role-policy --role-name GPUAutoscalerRole \
  --policy-name GPUAutoscalerPolicy \
  --policy-document file://autoscaler-policy.json
```

2. **Create Auto Scaling Groups**:
```bash
aws autoscaling create-auto-scaling-group \
  --auto-scaling-group-name gpu-spot-asg \
  --launch-template LaunchTemplateName=gpu-spot-template \
  --min-size 0 \
  --max-size 50 \
  --vpc-zone-identifier subnet-xxx,subnet-yyy,subnet-zzz
```

3. **Update values.yaml**:
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

1. **Create Service Account**:
```bash
gcloud iam service-accounts create gpu-autoscaler \
  --display-name="GPU Autoscaler"

gcloud projects add-iam-policy-binding PROJECT_ID \
  --member="serviceAccount:gpu-autoscaler@PROJECT_ID.iam.gserviceaccount.com" \
  --role="roles/compute.instanceAdmin.v1"
```

2. **Create Managed Instance Groups**:
```bash
gcloud compute instance-groups managed create gpu-spot-mig \
  --size 0 \
  --template gpu-spot-template \
  --zone us-central1-a

gcloud compute instance-groups managed set-autoscaling gpu-spot-mig \
  --max-num-replicas 50 \
  --zone us-central1-a
```

### Azure

1. **Create Service Principal**:
```bash
az ad sp create-for-rbac --name gpu-autoscaler

az role assignment create \
  --role "Virtual Machine Contributor" \
  --assignee <service-principal-id> \
  --scope /subscriptions/<subscription-id>
```

2. **Create VM Scale Sets**:
```bash
az vmss create \
  --name gpu-spot-vmss \
  --resource-group gpu-cluster \
  --image gpu-ubuntu-image \
  --priority Spot \
  --eviction-policy Delete \
  --instance-count 0 \
  --vm-sku Standard_NC6s_v3
```

## Advanced Scenarios

### Multi-Region Autoscaling

Deploy separate policies per region:

```yaml
# us-west-2
apiVersion: gpu-autoscaler.io/v1alpha1
kind: AutoscalingPolicy
metadata:
  name: us-west-2-autoscaling
spec:
  nodeSelector:
    topology.kubernetes.io/region: us-west-2
  ...

---
# us-east-1
apiVersion: gpu-autoscaler.io/v1alpha1
kind: AutoscalingPolicy
metadata:
  name: us-east-1-autoscaling
spec:
  nodeSelector:
    topology.kubernetes.io/region: us-east-1
  ...
```

### Per-Team Autoscaling

Separate policies per team:

```yaml
apiVersion: gpu-autoscaler.io/v1alpha1
kind: AutoscalingPolicy
metadata:
  name: ml-team-autoscaling
spec:
  nodeSelector:
    team: ml
  minNodes: 5
  maxNodes: 50
  spotInstancePercentage: 0.7

---
apiVersion: gpu-autoscaler.io/v1alpha1
kind: AutoscalingPolicy
metadata:
  name: research-team-autoscaling
spec:
  nodeSelector:
    team: research
  minNodes: 2
  maxNodes: 20
  spotInstancePercentage: 0.5
```

### GPU Type Specific

Different policies for different GPU types:

```yaml
# V100 policy
apiVersion: gpu-autoscaler.io/v1alpha1
kind: AutoscalingPolicy
metadata:
  name: v100-autoscaling
spec:
  nodeSelector:
    gpu-type: v100
  nodePools:
    - instanceTypes: [p3.2xlarge, p3.8xlarge]
  ...

---
# A100 policy
apiVersion: gpu-autoscaler.io/v1alpha1
kind: AutoscalingPolicy
metadata:
  name: a100-autoscaling
spec:
  nodeSelector:
    gpu-type: a100
  nodePools:
    - instanceTypes: [p4d.24xlarge]
  ...
```

## Troubleshooting

### Autoscaler Not Scaling

1. Check controller logs:
```bash
kubectl logs -n gpu-autoscaler-system deployment/gpu-autoscaler-controller
```

2. Check policy status:
```bash
kubectl describe autoscalingpolicy <policy-name>
```

3. Check cooldown:
```bash
# Query Prometheus
gpu_autoscaler_scale_up_cooldown_remaining_seconds
gpu_autoscaler_scale_down_cooldown_remaining_seconds
```

### High Spot Interruption Rate

1. Check interruption rate:
```bash
# Query Prometheus
rate(gpu_autoscaler_spot_interruptions_total[24h])
```

2. If >10%, consider:
   - Reduce `spotInstancePercentage`
   - Add more instance types to `nodePools`
   - Use different availability zones

### Cost Higher Than Expected

1. Check node distribution:
```bash
kubectl get nodes -L gpu-autoscaler.io/capacity-type
```

2. Verify spot percentage:
```bash
# Query Prometheus
gpu_autoscaler_node_count{capacity_type="spot"} /
  sum(gpu_autoscaler_node_count)
```

3. Check if nodes are scaling down:
```bash
# Query Prometheus
gpu_autoscaler_underutilized_nodes
```

## Best Practices

1. **Start Conservative**: Begin with higher thresholds and longer cooldowns
2. **Monitor First**: Observe patterns for 1-2 weeks before enabling predictive scaling
3. **Use PDBs**: Protect critical workloads with PodDisruptionBudgets
4. **Label Workloads**: Add eviction priority labels to guide spot termination
5. **Test Eviction**: Simulate spot termination to verify graceful handling
6. **Diversify**: Use multiple instance types in spot pools
7. **Set Budgets**: Use `maxNodes` to prevent runaway scaling

## See Also

- [Phase 3 Documentation](../../docs/phase3-autoscaling-engine.md)
- [Phase 1: Observability](../phase1/)
- [Phase 2: Workload Packing](../phase2/)
- [Monitoring Guide](../../docs/monitoring.md)
