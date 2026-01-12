# Phase 2: Intelligent Packing

Phase 2 of GPU Autoscaler introduces intelligent workload consolidation and GPU sharing mechanisms to maximize GPU utilization and reduce costs.

## Overview

Phase 2 delivers five key features:

1. **Bin-Packing Algorithm**: Automatically consolidate GPU workloads for optimal resource usage
2. **NVIDIA MIG Support**: Hardware-level GPU partitioning for A100/H100 GPUs
3. **NVIDIA MPS Support**: Process-level GPU sharing for inference workloads
4. **Time-Slicing**: Software-based GPU sharing for compatible workloads
5. **Admission Webhook**: Zero-touch optimization requiring no workload changes

## Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│                    Admission Webhook                             │
│  (Intercepts pod creation, applies optimization automatically)   │
└────────────┬────────────────────────────────────────────────────┘
             │
             ├─────► MIG Manager (Hardware partitioning)
             ├─────► MPS Manager (Process sharing)
             └─────► Time-Slicing Manager (Temporal sharing)

┌─────────────────────────────────────────────────────────────────┐
│              Bin-Packing Scheduler                               │
│  (Analyzes cluster, identifies consolidation opportunities)      │
└─────────────────────────────────────────────────────────────────┘
```

## 1. Bin-Packing Algorithm

### Overview

The bin-packing algorithm consolidates GPU workloads to minimize the number of nodes required, reducing costs and fragmentation.

### Strategies

- **BestFit**: Pack workloads into the most utilized node that can fit them (default)
- **FirstFit**: Pack workloads into the first node that can fit them
- **WorstFit**: Pack workloads into the least utilized node

### Configuration

```yaml
# values.yaml
binPacking:
  enabled: true
  strategy: bestfit
  analysisInterval: 5m
  autoConsolidate: false  # Manual approval for safety
```

### How It Works

1. Collects all pending GPU workloads
2. Sorts by priority and GPU requirements
3. Applies packing strategy to select optimal node placement
4. Reports consolidation opportunities and potential savings

### Example Usage

```bash
# View consolidation opportunities
kubectl logs -n gpu-autoscaler-system deploy/gpu-autoscaler-controller \
  | grep "Consolidation opportunities"

# Example output:
# Consolidation opportunities found: underutilizedNodes=3, potentialSavings=12 GPUs
# Recommendation: Node gpu-node-02 is underutilized (25.0% used, 2/8 GPUs allocated)
```

## 2. NVIDIA MIG (Multi-Instance GPU)

### Overview

MIG enables hardware-level GPU partitioning on A100 and H100 GPUs, providing isolated GPU instances with guaranteed memory and compute.

### Supported Profiles

**A100-40GB:**
- `1g.5gb`: 1/7th GPU, 5GB memory
- `2g.10gb`: 2/7ths GPU, 10GB memory
- `3g.20gb`: 3/7ths GPU, 20GB memory
- `4g.20gb`: 4/7ths GPU, 20GB memory
- `7g.40gb`: Full GPU, 40GB memory

**A100-80GB:**
- `1g.10gb`: 1/7th GPU, 10GB memory
- `2g.20gb`: 2/7ths GPU, 20GB memory
- `3g.40gb`: 3/7ths GPU, 40GB memory
- `4g.40gb`: 4/7ths GPU, 40GB memory
- `7g.80gb`: Full GPU, 80GB memory

### When to Use MIG

✅ **Ideal for:**
- Small batch processing workloads
- Workloads requiring <20GB GPU memory
- Applications needing hardware isolation
- Multi-tenant environments

❌ **Not suitable for:**
- Training workloads requiring full GPU
- Applications using P2P communication
- Workloads >40GB memory on 40GB A100

### Configuration

```yaml
# values.yaml
sharing:
  mig:
    enabled: true
    autoConfig: true
    defaultProfile: "1g.5gb"
    nodeSelector:
      nvidia.com/mig.capable: "true"

admissionWebhook:
  optimization:
    enableMIG: true
```

### Manual Node Configuration

```yaml
apiVersion: gpuautoscaler.io/v1alpha1
kind: GPUNodeConfig
metadata:
  name: gpu-node-01-config
spec:
  nodeName: gpu-node-01
  migEnabled: true
  migProfiles:
    - "1g.5gb"
    - "2g.10gb"
```

### Policy-Based Configuration

```yaml
apiVersion: gpuautoscaler.io/v1alpha1
kind: GPUSharingPolicy
metadata:
  name: small-workloads-mig
spec:
  strategy: mig
  podSelector:
    matchLabels:
      gpu-autoscaler.io/workload-type: batch
  priority: 10
  migConfig:
    autoSelectProfile: true
```

### Pod Annotation

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: batch-job
  annotations:
    gpu-autoscaler.io/sharing: enabled
    gpu-autoscaler.io/sharing-mode: mig
    gpu-autoscaler.io/mig-profile: "1g.5gb"  # Optional explicit profile
spec:
  containers:
  - name: processor
    resources:
      requests:
        nvidia.com/gpu: 1
```

## 3. NVIDIA MPS (Multi-Process Service)

### Overview

MPS enables multiple CUDA processes to share a single GPU with improved concurrency, ideal for inference workloads.

### When to Use MPS

✅ **Ideal for:**
- Inference workloads with <50% GPU utilization
- Multiple concurrent processes on same GPU
- Model serving applications
- Real-time inference services

❌ **Not suitable for:**
- Training workloads
- Applications with high GPU utilization (>60%)
- Workloads requiring exclusive GPU access

### Configuration

```yaml
# values.yaml
sharing:
  mps:
    enabled: true
    maxClients: 16
    defaultActiveThreads: 100
    resources:
      memoryLimitMB: 2048
    nodeSelector:
      nvidia.com/mps.capable: "true"

admissionWebhook:
  optimization:
    enableMPS: true
```

### Policy-Based Configuration

```yaml
apiVersion: gpuautoscaler.io/v1alpha1
kind: GPUSharingPolicy
metadata:
  name: inference-mps
spec:
  strategy: mps
  podSelector:
    matchLabels:
      gpu-autoscaler.io/workload-type: inference
  priority: 10
  mpsConfig:
    maxClients: 16
    memoryLimitMB: 2048
```

### Pod Labels

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: inference-server
  labels:
    gpu-autoscaler.io/workload-type: inference
  annotations:
    gpu-autoscaler.io/sharing: enabled
spec:
  containers:
  - name: server
    image: pytorch/torchserve:latest
    resources:
      requests:
        nvidia.com/gpu: 1
```

### Monitoring MPS

```bash
# Check MPS status on a node
kubectl get gpunodeconfig gpu-node-02-config -o jsonpath='{.status.mpsStatus}'

# Example output:
# {
#   "enabled": true,
#   "activeClients": 8,
#   "maxClients": 16
# }
```

## 4. Time-Slicing

### Overview

Time-slicing enables temporal multiplexing of GPU resources, allowing multiple workloads to time-share a single GPU.

### When to Use Time-Slicing

✅ **Ideal for:**
- Development and testing workloads
- Jupyter notebooks
- Batch processing with short GPU operations
- Interactive workloads with <60% utilization

❌ **Not suitable for:**
- Training workloads requiring sustained GPU access
- Real-time inference with latency requirements
- High-performance computing workloads

### Configuration

```yaml
# values.yaml
sharing:
  timeSlicing:
    enabled: true
    replicasPerGPU: 4
    sliceMs: 100
    fairnessMode: roundrobin
    oversubscribe: false

admissionWebhook:
  optimization:
    enableTimeSlicing: true
```

### Policy-Based Configuration

```yaml
apiVersion: gpuautoscaler.io/v1alpha1
kind: GPUSharingPolicy
metadata:
  name: development-timeslicing
spec:
  strategy: timeslicing
  podSelector:
    matchLabels:
      gpu-autoscaler.io/workload-type: development
  priority: 5
  timeSlicingConfig:
    replicasPerGPU: 4
    sliceMs: 100
    fairnessMode: roundrobin
```

### Fairness Modes

- **roundrobin**: Equal time slices for all workloads (default)
- **priority**: Time allocated based on pod priority
- **weighted**: Time allocated based on resource requests

### Optimal Replicas Calculation

The system automatically calculates optimal replicas based on:
- Average GPU utilization
- Workload burstiness
- Historical usage patterns

| Avg Utilization | Burstiness | Recommended Replicas |
|-----------------|------------|---------------------|
| <20%            | >70%       | 8                   |
| 20-40%          | >50%       | 4                   |
| 40-60%          | Any        | 2                   |
| >60%            | Any        | 1 (no sharing)      |

## 5. Admission Webhook

### Overview

The admission webhook provides zero-touch optimization by automatically selecting and applying the best GPU sharing strategy when pods are created.

### How It Works

1. Intercepts pod creation requests
2. Analyzes workload characteristics:
   - GPU and memory requirements
   - Workload type (from labels)
   - Historical utilization patterns
3. Selects optimal sharing strategy:
   - MIG for small workloads (<20GB memory)
   - MPS for inference workloads
   - Time-slicing for dev/batch workloads
   - Exclusive for training workloads
4. Mutates pod spec to use selected strategy
5. Adds optimization metadata annotations

### Decision Flow

```
Pod Creation
    │
    ├─> Training workload? ──────► Exclusive GPU
    │
    ├─> GPU=1 && Memory<20GB? ───► MIG
    │
    ├─> Inference workload? ──────► MPS
    │
    └─> Dev/Batch workload? ──────► Time-Slicing
```

### Configuration

```yaml
# values.yaml
admissionWebhook:
  enabled: true
  rewriteRequests: true
  failurePolicy: Ignore  # Fail open for safety

  optimization:
    enableMIG: true
    enableMPS: true
    enableTimeSlicing: true
    autoDetectWorkloadType: true

  exemptNamespaces:
    - kube-system
    - production  # Add namespaces to skip optimization
```

### Workload Type Labels

The webhook uses these labels for automatic detection:

```yaml
labels:
  gpu-autoscaler.io/workload-type: <type>
```

**Supported types:**
- `training`: ML training (exclusive GPU)
- `inference`: Model inference (MPS)
- `development`: Development/testing (time-slicing)
- `batch`: Batch processing (MIG or time-slicing)
- `serving`: Model serving (MPS)

### Opting Out

Disable optimization for specific pods:

```yaml
metadata:
  annotations:
    gpu-autoscaler.io/optimize: "false"
```

Disable specific strategy:

```yaml
metadata:
  annotations:
    gpu-autoscaler.io/sharing: disabled
    # or
    gpu-autoscaler.io/mps-enabled: "false"
```

## Monitoring and Observability

### Grafana Dashboards

Phase 2 extends existing dashboards with:
- GPU sharing efficiency metrics
- MIG device utilization
- MPS client statistics
- Time-slicing performance
- Consolidation opportunities

### CLI Commands

```bash
# View optimization statistics
kubectl logs -n gpu-autoscaler-system deploy/gpu-autoscaler-controller \
  | grep "savings estimate"

# Check GPU sharing policies
kubectl get gpusharingpolicies

# View node configurations
kubectl get gpunodeconfigs

# Check webhook statistics
kubectl logs -n gpu-autoscaler-system deploy/gpu-autoscaler-webhook \
  | grep "optimization"
```

### Prometheus Metrics

```promql
# MIG savings
gpu_autoscaler_mig_eligible_pods
gpu_autoscaler_mig_savings_pct

# MPS savings
gpu_autoscaler_mps_eligible_pods
gpu_autoscaler_mps_active_clients

# Time-slicing savings
gpu_autoscaler_timeslicing_virtual_gpus
gpu_autoscaler_timeslicing_savings_pct

# Bin-packing
gpu_autoscaler_consolidation_opportunities
gpu_autoscaler_cluster_fragmentation
```

## Cost Savings Examples

### Example 1: Inference Workloads with MPS

**Before Phase 2:**
- 10 inference pods
- Each requesting 1 full GPU
- Total: 10 GPUs required

**After Phase 2 with MPS:**
- 10 inference pods sharing 2 GPUs (8:1 ratio)
- Total: 2 GPUs required
- **Savings: 80% (8 GPUs)**

### Example 2: Batch Processing with MIG

**Before Phase 2:**
- 20 small batch jobs
- Each requesting 1 full A100 GPU
- Total: 20 A100 GPUs

**After Phase 2 with MIG:**
- 20 jobs using 1g.5gb MIG profiles
- 7 jobs per physical GPU
- Total: 3 A100 GPUs required
- **Savings: 85% (17 GPUs)**

### Example 3: Development with Time-Slicing

**Before Phase 2:**
- 16 Jupyter notebooks
- Each requesting 1 GPU
- Total: 16 GPUs

**After Phase 2 with Time-Slicing (4x):**
- 16 notebooks sharing 4 GPUs
- Total: 4 GPUs required
- **Savings: 75% (12 GPUs)**

## Best Practices

### 1. Label Your Workloads

Always label pods with workload type for optimal automation:

```yaml
labels:
  gpu-autoscaler.io/workload-type: inference
```

### 2. Start with Policies

Use GPUSharingPolicies for namespace-wide or cluster-wide defaults:

```bash
kubectl apply -f examples/phase2/gpu-sharing-policy-mps.yaml
```

### 3. Monitor Before Automating

Enable analysis mode first:

```yaml
binPacking:
  autoConsolidate: false  # Monitor recommendations first
```

### 4. Use Node Selectors

Dedicate specific nodes for different strategies:

```yaml
# MIG nodes
nvidia.com/mig.capable: "true"

# MPS nodes for inference
nvidia.com/mps.capable: "true"

# Time-slicing for dev
environment: development
```

### 5. Test in Non-Production

Test Phase 2 features in dev/staging before production:

```yaml
namespaceSelector:
  matchLabels:
    environment: staging
```

## Troubleshooting

### Webhook Not Mutating Pods

Check webhook configuration:

```bash
kubectl get mutatingwebhookconfiguration gpu-autoscaler-webhook
kubectl logs -n gpu-autoscaler-system deploy/gpu-autoscaler-webhook
```

Verify TLS certificates:

```bash
kubectl get secret -n gpu-autoscaler-system webhook-server-cert
```

### MIG Devices Not Available

Check node labels:

```bash
kubectl get node <node-name> -o jsonpath='{.metadata.labels}'
```

Verify NVIDIA device plugin configuration:

```bash
kubectl get daemonset -n kube-system nvidia-device-plugin-daemonset
```

### MPS Not Working

Check MPS daemon on node:

```bash
kubectl exec -n gpu-autoscaler-system <dcgm-pod> -- nvidia-smi
```

Verify node annotations:

```bash
kubectl get node <node-name> -o jsonpath='{.metadata.annotations}'
```

## Migration from Phase 1

Phase 2 is backward compatible with Phase 1. To migrate:

1. **Update Helm chart:**
   ```bash
   helm upgrade gpu-autoscaler ./charts/gpu-autoscaler \
     --set sharing.mig.enabled=false \
     --set sharing.mps.enabled=false \
     --set sharing.timeSlicing.enabled=false
   ```

2. **Enable features gradually:**
   - Start with bin-packing analysis
   - Enable time-slicing for dev
   - Enable MPS for inference
   - Enable MIG last (requires node restart)

3. **Add workload labels:**
   ```bash
   kubectl label pod <pod-name> gpu-autoscaler.io/workload-type=inference
   ```

4. **Monitor savings:**
   ```bash
   kubectl logs -n gpu-autoscaler-system deploy/gpu-autoscaler-controller \
     | grep "savings"
   ```

## Next Steps

- **Phase 3**: Multi-cloud autoscaling with spot instance management
- **Phase 4**: Cost tracking and attribution with TimescaleDB
- **Phase 5**: Advanced scheduling with ML-based predictions

## Support

For issues or questions:
- GitHub: https://github.com/holynakamoto/gpuautoscaler/issues
- Documentation: /docs/
- Examples: /examples/phase2/
