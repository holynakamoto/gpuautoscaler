# Architecture Overview

This document describes the architecture of GPU Autoscaler and how its components work together.

## High-Level Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                   GPU Kubernetes Cluster                     │
│  ┌────────────┐  ┌────────────┐  ┌────────────┐            │
│  │ GPU Node   │  │ GPU Node   │  │ GPU Node   │            │
│  │ + DCGM     │  │ + DCGM     │  │ + DCGM     │            │
│  │ + ML Pods  │  │ + ML Pods  │  │ + ML Pods  │            │
│  └────────────┘  └────────────┘  └────────────┘            │
└──────────────────────┬──────────────────────────────────────┘
                       │ (scrape GPU metrics)
                       ▼
┌─────────────────────────────────────────────────────────────┐
│              Observability Layer                             │
│  ┌──────────────────────────────────────────────────────┐   │
│  │  Prometheus (time-series metrics storage)           │   │
│  │  - GPU utilization, memory, power, temperature      │   │
│  │  - Kubernetes pod/node metadata                     │   │
│  └──────────────────────────────────────────────────────┘   │
│                           │                                  │
│                           ▼                                  │
│  ┌──────────────────────────────────────────────────────┐   │
│  │  Grafana (visualization)                            │   │
│  │  - Pre-built dashboards                             │   │
│  │  - Real-time monitoring                             │   │
│  └──────────────────────────────────────────────────────┘   │
└──────────────────────────┬───────────────────────────────────┘
                           │ (metrics queries)
                           ▼
┌─────────────────────────────────────────────────────────────┐
│              GPU Autoscaler Control Plane                    │
│                                                               │
│  ┌─────────────────────────────────────────────────────┐    │
│  │  GPU Controller (Kubernetes Controller)            │    │
│  │  ┌──────────────┐  ┌──────────────┐  ┌───────────┐ │    │
│  │  │ Metrics      │  │  Waste       │  │  Event    │ │    │
│  │  │ Analyzer     │→ │  Detector    │→ │  Creator  │ │    │
│  │  └──────────────┘  └──────────────┘  └───────────┘ │    │
│  └─────────────────────────────────────────────────────┘    │
│                           │                                  │
│  ┌─────────────────────────────────────────────────────┐    │
│  │  Admission Webhook (Optional - Phase 2)            │    │
│  │  - Intercept pod creation                           │    │
│  │  - Rewrite GPU requests                             │    │
│  │  - Add sharing annotations                          │    │
│  └─────────────────────────────────────────────────────┘    │
│                           │                                  │
│  ┌─────────────────────────────────────────────────────┐    │
│  │  Autoscaling Engine (Optional - Phase 3)           │    │
│  │  - Monitor pending pods                             │    │
│  │  - Scale GPU nodes up/down                          │    │
│  │  - Spot instance orchestration                      │    │
│  └─────────────────────────────────────────────────────┘    │
│                           │                                  │
│  ┌─────────────────────────────────────────────────────┐    │
│  │  Cost Calculator (Optional - Phase 4)              │    │
│  │  - Track GPU usage per pod/namespace               │    │
│  │  - Calculate costs using cloud pricing             │    │
│  │  - Store in TimescaleDB                            │    │
│  └─────────────────────────────────────────────────────┘    │
└───────────────────────────┬──────────────────────────────────┘
                            │
                            ▼
┌─────────────────────────────────────────────────────────────┐
│                  User Interfaces                             │
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐      │
│  │   Grafana    │  │     CLI      │  │   K8s        │      │
│  │  Dashboards  │  │ (gpu-auto    │  │  Events      │      │
│  │              │  │  -scaler)    │  │              │      │
│  └──────────────┘  └──────────────┘  └──────────────┘      │
└─────────────────────────────────────────────────────────────┘
```

## Components

### 1. DCGM Exporter (Data Collection)

**Purpose**: Collect GPU metrics from NVIDIA GPUs on each node

**Technology**:
- NVIDIA DCGM (Data Center GPU Manager)
- Prometheus exporter

**Deployment**:
- DaemonSet (runs on every GPU node)
- Privileged container for hardware access

**Metrics Collected**:
- `DCGM_FI_DEV_GPU_UTIL`: GPU utilization (0-100%)
- `DCGM_FI_DEV_FB_USED`: GPU memory used (MB)
- `DCGM_FI_DEV_FB_TOTAL`: Total GPU memory (MB)
- `DCGM_FI_DEV_POWER_USAGE`: Power consumption (Watts)
- `DCGM_FI_DEV_GPU_TEMP`: GPU temperature (Celsius)
- `DCGM_FI_DEV_SM_CLOCK`: SM clock frequency (MHz)
- `DCGM_FI_DEV_MEM_CLOCK`: Memory clock frequency (MHz)

**Key Features**:
- Kubernetes pod attribution (links metrics to pods)
- Multi-GPU support (separate metrics per GPU)
- 10-second scrape interval (configurable)

### 2. Prometheus (Metrics Storage)

**Purpose**: Time-series database for GPU metrics

**Features**:
- Scrapes DCGM exporter via ServiceMonitor
- Stores metrics with configurable retention (7 days default)
- Provides PromQL query interface
- Federation support for multi-cluster (future)

**Data Retention**:
- Raw metrics: 7 days default
- Aggregated metrics: 30 days (for cost reporting)
- Configurable via Helm values

### 3. Grafana (Visualization)

**Purpose**: Dashboard and visualization for GPU metrics

**Pre-built Dashboards**:
1. **GPU Cluster Overview**
   - Total GPU count and utilization
   - Utilization heatmap across nodes
   - Average metrics (GPU, memory, power)

2. **Namespace View** (Future)
   - Per-team GPU usage
   - Cost attribution
   - Waste analysis

3. **Pod Detail** (Future)
   - Individual workload performance
   - GPU timeline
   - Optimization recommendations

### 4. GPU Controller (Core Logic)

**Purpose**: Kubernetes controller that watches GPU pods and analyzes utilization

**Implementation**:
- Go using controller-runtime framework
- Reconciles on pod create/update/delete events
- Leader election for HA (3 replicas)

**Responsibilities**:
1. **Metrics Analysis**
   - Query Prometheus for GPU metrics
   - Join with Kubernetes pod metadata
   - Calculate utilization patterns

2. **Waste Detection**
   - Identify underutilized GPUs (<50% util for >5 minutes)
   - Calculate waste score (0-100)
   - Generate optimization recommendations

3. **Event Creation**
   - Create Kubernetes events with recommendations
   - Alert users to optimization opportunities
   - Track optimization actions

**Reconciliation Loop**:
```
Every 30 seconds:
  For each GPU pod:
    1. Get pod details from K8s API
    2. Query GPU metrics from Prometheus
    3. Calculate utilization over last 10 minutes
    4. If waste detected (score > 50):
       - Log recommendation
       - Create K8s event
       - Track in metrics
```

### 5. Admission Webhook (Phase 2)

**Purpose**: Automatically optimize GPU requests when pods are created

**Implementation**:
- Mutating webhook server (HTTPS)
- Intercepts pod creation requests
- Rewrites GPU resource requests

**Logic**:
```
On pod creation:
  1. Extract GPU request (e.g., 1 GPU)
  2. Check historical usage patterns for similar pods
  3. If historically uses <50% GPU:
     - Rewrite to use MIG slice (e.g., 1g.5gb)
     - Add annotation: gpu-autoscaler.io/optimized=true
  4. If sharing is enabled for namespace:
     - Add MPS annotations
  5. Return modified pod spec
```

**Configuration**:
- Opt-in/opt-out per namespace via labels
- Override via pod annotations
- Fail-open policy (if webhook down, allow pod creation)

### 6. Autoscaling Engine (Phase 3)

**Purpose**: Scale GPU nodes up/down based on demand

**Implementation**:
- Custom controller watching pod queue and node utilization
- Integration with cloud provider APIs (AWS ASG, GCP MIG, Azure VMSS)

**Scale-Up Triggers**:
- Pending GPU pods for >2 minutes
- Cluster GPU utilization >80%
- Priority-based (high-priority pods trigger faster)

**Scale-Down Triggers**:
- Node idle for >10 minutes (no running pods)
- Node GPU utilization <20%
- Respects PodDisruptionBudgets

**Spot Instance Strategy**:
- Prioritize spot instances for 60% of workloads
- Diversify across instance types to reduce interruption risk
- Graceful eviction on termination notice (2-minute warning)
- Auto-restart jobs on alternative nodes

### 7. Cost Calculator (Phase 4)

**Purpose**: Track and attribute GPU costs

**Implementation**:
- Queries cloud pricing APIs (AWS, GCP, Azure)
- Calculates cost per pod per second
- Stores in TimescaleDB for historical analysis

**Cost Attribution**:
```
Cost = (Node hourly rate / GPUs per node) × (Usage duration in hours) × (Utilization %)

Attribution:
- By namespace (default)
- By label (gpu-cost-center, gpu-project)
- By custom rules (e.g., split shared GPUs proportionally)
```

**Features**:
- Real-time cost dashboard in Grafana
- Budget alerts (soft limit at 80%, hard limit at 100%)
- Savings reports (baseline vs. optimized)

### 8. CLI Tool

**Purpose**: Command-line interface for users

**Commands**:
- `gpu-autoscaler status`: Show cluster GPU utilization
- `gpu-autoscaler optimize`: Analyze and recommend optimizations
- `gpu-autoscaler cost`: Show cost breakdown
- `gpu-autoscaler report`: Generate comprehensive reports

**Implementation**:
- Go using cobra framework
- Queries Prometheus and K8s API directly
- Formats output as tables or JSON

## Data Flow

### Monitoring Flow

1. **Collection** (every 10s):
   - DCGM exporter reads GPU metrics from NVIDIA drivers
   - Adds Kubernetes labels (node, pod, namespace)
   - Exposes via HTTP `/metrics` endpoint

2. **Storage** (every 10s):
   - Prometheus scrapes DCGM exporter
   - Stores metrics in time-series database
   - Applies retention policies

3. **Analysis** (every 30s):
   - GPU controller queries Prometheus
   - Calculates utilization patterns
   - Detects waste and generates recommendations

4. **Visualization** (real-time):
   - Grafana queries Prometheus
   - Renders dashboards
   - Displays alerts

### Optimization Flow

1. **Detection**:
   - Controller identifies underutilized pod
   - Calculates waste score and recommendation
   - Creates Kubernetes event

2. **Notification**:
   - User sees event in `kubectl describe pod`
   - Dashboard shows optimization opportunities
   - (Optional) Slack/email alert

3. **Action** (manual in Phase 1):
   - User adds annotation: `gpu-autoscaler.io/sharing=enabled`
   - Re-creates pod with optimized configuration

4. **Verification**:
   - Controller monitors new pod
   - Tracks utilization improvement
   - Reports savings in dashboard

### Autoscaling Flow (Phase 3)

1. **Scale-Up**:
   - New GPU pod submitted, goes to Pending state
   - Autoscaler detects pending pod after 2 minutes
   - Determines GPU type and count needed
   - Checks spot instance availability
   - Calls cloud provider API to add nodes
   - Node joins cluster, pod gets scheduled

2. **Scale-Down**:
   - Autoscaler detects idle node (>10 minutes, <20% util)
   - Checks for running pods (none)
   - Cordons node (prevent new pods)
   - Drains node gracefully (respects PDBs)
   - Calls cloud provider API to remove node

## Technology Stack

### Core Services
- **Language**: Go 1.21+
- **Frameworks**:
  - controller-runtime (Kubernetes controllers)
  - cobra (CLI)
  - prometheus/client_golang (metrics)

### Dependencies
- **NVIDIA DCGM**: 3.x
- **Prometheus**: 2.x
- **Grafana**: 10.x
- **Kubernetes**: 1.25+

### Cloud Integrations
- **AWS**: EC2 API, Auto Scaling Groups
- **GCP**: Compute Engine API, Managed Instance Groups
- **Azure**: VM Scale Sets API

## Security

### RBAC Permissions
- **Controller**:
  - Read: pods, nodes, configmaps
  - Write: events, leases (for leader election)
  - Update: pod annotations (for optimization tracking)

- **DCGM Exporter**:
  - Privileged: yes (for GPU access)
  - HostPath: /var/lib/kubelet/pod-resources (read-only)

### Network Policies
- Controller → Prometheus: port 9090
- Prometheus → DCGM Exporter: port 9400
- Grafana → Prometheus: port 9090
- External → Grafana: port 80 (via LoadBalancer/Ingress)

### Webhook Security
- TLS required (cert-manager integration)
- mTLS optional (for enhanced security)
- Fail-open policy (availability over enforcement)

## Performance

### Resource Usage
- **Controller**: 100m CPU, 128Mi memory (per replica)
- **DCGM Exporter**: 100m CPU, 128Mi memory (per node)
- **Prometheus**: 500m CPU, 1Gi memory (adjusts with data volume)
- **Grafana**: 100m CPU, 128Mi memory

### Scalability
- Tested with: 100 GPU nodes, 500 pods
- Target: 1,000 GPU nodes, 10,000 pods
- Bottlenecks: Prometheus storage, controller reconciliation speed

### Overhead
- DCGM metrics collection: <1% GPU performance impact
- Controller reconciliation: <100ms per pod
- Webhook latency: <50ms p99

## High Availability

### Controller HA
- 3 replicas with leader election
- Only one replica active at a time
- Automatic failover (<10s)

### Prometheus HA (optional)
- Use Thanos or Cortex for federation
- Remote write to long-term storage

### Grafana HA (optional)
- Multiple replicas behind load balancer
- Shared database (PostgreSQL)

## Monitoring the Monitor

### Controller Health
- Metrics: `controller_reconcile_errors`, `controller_reconcile_duration`
- Liveness probe: `/healthz` (checks controller loop)
- Readiness probe: `/readyz` (checks Prometheus connectivity)

### DCGM Exporter Health
- Metrics: `dcgm_up` (1 if DCGM accessible, 0 otherwise)
- Restart policy: Always (auto-restart on failure)

### Prometheus Health
- Self-monitoring: Query `up` metric for DCGM exporters
- Alerts: Prometheus down, scrape failures

## Future Enhancements

### Phase 5: Multi-Cluster
- Centralized control plane managing multiple clusters
- Cross-cluster workload migration
- Global cost optimization

### Phase 6: Advanced Scheduling
- Gang scheduling for multi-GPU training
- GPU topology awareness (NVLink, NVSwitch)
- Predictive autoscaling using ML

### Phase 7: Integration
- MLflow integration for experiment tracking
- Argo Workflows integration for pipeline optimization
- Custom metrics framework for application-level optimization
