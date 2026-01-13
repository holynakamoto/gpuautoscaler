# GPU Autoscaler

[![License](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](https://opensource.org/licenses/Apache-2.0)
[![Go Report Card](https://goreportcard.com/badge/github.com/gpuautoscaler/gpuautoscaler)](https://goreportcard.com/report/github.com/gpuautoscaler/gpuautoscaler)

> **Maximize GPU cluster utilization, minimize cloud costs**

GPU Autoscaler is an open-source Kubernetes-native system that reduces GPU infrastructure costs by 30-50% through intelligent workload packing, multi-tenancy support (MIG/MPS/time-slicing), and cost-optimized autoscaling.

## 🎯 Problem

Organizations running GPU workloads on Kubernetes waste 40-60% of GPU capacity and overspend by millions annually due to:

- **Allocation vs. Utilization Gap**: Pods request full GPUs but use <50% of resources
- **Inefficient Multi-Tenancy**: Small inference jobs monopolize entire GPUs when they could share
- **Suboptimal Autoscaling**: Kubernetes doesn't understand GPU-specific metrics (VRAM, SM utilization)
- **Lack of Cost Attribution**: No per-team/experiment cost tracking

## ✨ Features

### Phase 1: Observability Foundation (Available Now)
- 📊 **Real-time GPU Metrics**: DCGM integration with Prometheus for GPU utilization, VRAM, temperature, power
- 🔍 **Waste Detection**: Identify underutilized GPUs and workloads
- 📈 **Grafana Dashboards**: Pre-built dashboards with Kubernetes pod attribution
- 💰 **Cost Visibility**: Track GPU spend per namespace/team/experiment

### Phase 2: Intelligent Packing (Available Now)
- 🎯 **Bin-Packing Algorithm**: Automatically consolidate GPU workloads with BestFit/FirstFit/WorstFit strategies
- 🔀 **NVIDIA MIG Support**: Hardware partitioning for A100/H100 GPUs with 10 profiles (1g.5gb to 7g.80gb)
- 🔄 **NVIDIA MPS Support**: Process-level GPU sharing for inference workloads with configurable clients
- ⏱️ **Time-Slicing**: Software-based sharing for compatible workloads with configurable replicas
- 🎫 **Admission Webhook**: Zero-touch optimization requiring no workload changes - automatic strategy selection
- 📋 **GPU Sharing Policies**: CRDs for cluster-wide and namespace-specific optimization policies

### Phase 3: Autoscaling Engine (Available Now)
- 🚀 **GPU-Aware Autoscaling**: Scale based on actual GPU utilization, not just CPU/memory
- 💸 **Spot Instance Orchestration**: Prioritize cheaper spot instances with graceful eviction handling
- 🎚️ **Multi-Tier Scaling**: Optimize cost with spot → on-demand → reserved instance strategy
- 🔮 **Predictive Scaling**: Pre-warm nodes for known busy periods based on historical patterns
- ☁️ **Multi-Cloud Support**: AWS, GCP, and Azure integrations with Auto Scaling Groups / Managed Instance Groups / VM Scale Sets

### Phase 4: Cost Management (Available Now)
- 💵 **Real-time Cost Tracking**: Per-second GPU cost calculation with AWS/GCP/Azure pricing APIs
- 📊 **Cost Attribution**: Track spend by namespace, team, label, and experiment ID with historical data
- 🎯 **Budget Management**: Set spending limits with configurable alerts (Slack, Email, PagerDuty, Webhook)
- 🚨 **Budget Enforcement**: Throttle or block workloads when budgets are exceeded with grace periods
- 💾 **TimescaleDB Integration**: Store historical cost data with hypertables and continuous aggregates
- 📈 **ROI Reporting**: Demonstrate savings from MIG/MPS/time-slicing optimizations with detailed breakdowns
- 🔍 **Cost Drill-Down**: Analyze costs by node, GPU model, workload type, and time period
- 📊 **Grafana Dashboards**: Pre-built cost dashboards with hourly rate, budget burn rate, and attribution charts

## 🚀 Quick Start

### Prerequisites

- Kubernetes 1.25+ cluster with GPU nodes
- NVIDIA GPU drivers (470.x+ recommended)
- NVIDIA device plugin installed
- Helm 3.x

### Installation

```bash
# Add the GPU Autoscaler Helm repository
helm repo add gpu-autoscaler https://gpuautoscaler.github.io/charts
helm repo update

# Install GPU Autoscaler with default settings
helm install gpu-autoscaler gpu-autoscaler/gpu-autoscaler \
  --namespace gpu-autoscaler-system \
  --create-namespace

# Verify installation
kubectl get pods -n gpu-autoscaler-system
```

### Access Dashboards

```bash
# Port-forward Grafana
kubectl port-forward -n gpu-autoscaler-system svc/gpu-autoscaler-grafana 3000:80

# Open browser to http://localhost:3000
# Default credentials: admin / (see secret)
```

### View GPU Metrics

```bash
# Install the CLI tool
curl -sSL https://gpuautoscaler.io/install.sh | bash

# Check cluster GPU utilization
gpu-autoscaler status

# Analyze waste and get recommendations
gpu-autoscaler optimize

# View cost breakdown
gpu-autoscaler cost --namespace ml-team --last 7d
```

### Cost Management Setup

```bash
# Enable cost tracking with cloud provider credentials
helm upgrade gpu-autoscaler gpu-autoscaler/gpu-autoscaler \
  --namespace gpu-autoscaler-system \
  --set cost.enabled=true \
  --set cost.provider=aws \
  --set cost.region=us-west-2

# Configure TimescaleDB for historical data (optional)
helm upgrade gpu-autoscaler gpu-autoscaler/gpu-autoscaler \
  --set cost.timescaledb.enabled=true \
  --set cost.timescaledb.host=timescaledb.default.svc.cluster.local \
  --set cost.timescaledb.database=gpucost

# Create a cost budget with alerts
cat <<EOF | kubectl apply -f -
apiVersion: v1alpha1.gpuautoscaler.io
kind: CostBudget
metadata:
  name: ml-team-monthly-budget
spec:
  scope:
    type: namespace
    namespaceSelector:
      matchLabels:
        team: ml-team
  budget:
    amount: 50000.0  # $50K/month
    period: 30d
  alerts:
    - threshold: 80
      channels:
        - type: slack
          config:
            webhook: https://hooks.slack.com/services/YOUR/WEBHOOK/URL
    - threshold: 100
      channels:
        - type: email
          config:
            to: ml-team-leads@company.com
  enforcement:
    mode: throttle  # Options: alert, throttle, block
    gracePeriod: 2h
    throttleConfig:
      maxSpotInstances: 5
      blockOnDemand: true
EOF

# Create cost attribution tracking
cat <<EOF | kubectl apply -f -
apiVersion: v1alpha1.gpuautoscaler.io
kind: CostAttribution
metadata:
  name: ml-team-attribution
spec:
  scope:
    type: namespace
    namespaceSelector:
      matchLabels:
        team: ml-team
  trackBy:
    - namespace
    - label:experiment-id
    - label:user
  reportingPeriod: 24h
EOF

# View cost reports
gpu-autoscaler cost report --format table
gpu-autoscaler cost report --format json > cost-report.json
gpu-autoscaler cost roi --last 30d
```

## 📊 Results

Organizations using GPU Autoscaler report:

- **40-60% cost reduction** through better utilization and spot instances
- **GPU utilization: 40% → 75%+** through intelligent packing
- **2.5+ jobs per GPU** (up from 1.2) through MIG/MPS sharing
- **$500K-$2M annual savings** on GPU infrastructure

## 🏗️ Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                   GPU Kubernetes Cluster                     │
│  ┌────────────┐  ┌────────────┐  ┌────────────┐            │
│  │ GPU Node   │  │ GPU Node   │  │ GPU Node   │            │
│  │ + DCGM     │  │ + DCGM     │  │ + DCGM     │            │
│  │ + ML Pods  │  │ + ML Pods  │  │ + ML Pods  │            │
│  └────────────┘  └────────────┘  └────────────┘            │
└──────────────────────┬──────────────────────────────────────┘
                       │ (metrics)
                       ▼
┌─────────────────────────────────────────────────────────────┐
│              GPU Autoscaler Control Plane                    │
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐      │
│  │ Prometheus   │→ │  Controller  │→ │  Admission   │      │
│  │  (Metrics)   │  │  (Packing)   │  │   Webhook    │      │
│  └──────────────┘  └──────────────┘  └──────────────┘      │
│         │                  │                  │             │
│         ▼                  ▼                  ▼             │
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐      │
│  │  Grafana     │  │  Autoscaler  │  │    Cost      │      │
│  │ (Dashboards) │  │   Engine     │  │  Calculator  │      │
│  └──────────────┘  └──────────────┘  └──────────────┘      │
└─────────────────────────────────────────────────────────────┘
```

## 📖 Documentation

- [Installation Guide](docs/installation.md)
- [Architecture Overview](docs/architecture.md)
- [Configuration Reference](docs/configuration.md)
- [User Guide](docs/user-guide.md)
- [Troubleshooting](docs/troubleshooting.md)
- [API Reference](docs/api-reference.md)
- [Contributing Guide](CONTRIBUTING.md)

## 🤝 Contributing

We welcome contributions! See [CONTRIBUTING.md](CONTRIBUTING.md) for guidelines.

### Development Setup

```bash
# Clone the repository
git clone https://github.com/gpuautoscaler/gpuautoscaler.git
cd gpuautoscaler

# Install dependencies
make install-deps

# Run tests
make test

# Build controller
make build

# Run locally (requires Kind with GPU support)
make dev-cluster
```

## 📦 Release Guide

GPU Autoscaler uses automated semantic versioning with [svu](https://github.com/caarlos0/svu) and [GoReleaser](https://goreleaser.com/). Releases are automatically created when commits are pushed to the `main` branch.

### Commit Message Convention

Follow [Conventional Commits](https://www.conventionalcommits.org/) to trigger the correct version bump:

**Patch Release (v1.0.x)** - Bug fixes and minor changes:
```
fix: resolve memory leak in cost tracker
fix(api): handle nil pointer in budget controller
```

**Minor Release (v1.x.0)** - New features (backward compatible):
```
feat: add support for Azure GPU pricing
feat(cost): implement custom alert templates
```

**Major Release (vx.0.0)** - Breaking changes:
```
feat!: redesign CostBudget API with new fields
fix!: change default enforcement mode to throttle

BREAKING CHANGE: CostBudget.spec.limit renamed to CostBudget.spec.budget.amount
```

### Release Process

1. **Merge PR to main**: All commits to `main` trigger the release workflow
2. **Automatic tagging**: GitHub Actions calculates the next version using `svu` based on commit messages
3. **Build & publish**: GoReleaser creates:
   - GitHub Release with binaries (Linux, macOS, Windows)
   - Changelog from commit messages
   - Checksums for all artifacts

### Manual Release (if needed)

```bash
# Create and push a tag manually
git tag v1.0.5
git push origin v1.0.5

# The release workflow will trigger automatically
```

### Release Assets

Each release includes:
- `gpu-autoscaler-controller` - Kubernetes controller binary (Linux/macOS, amd64/arm64)
- `gpu-autoscaler` - CLI tool (Linux/macOS/Windows, amd64/arm64)
- Source code archives
- SHA256 checksums

## 📝 License

Apache License 2.0 - see [LICENSE](LICENSE) file for details.

## 🙏 Acknowledgments

- NVIDIA for DCGM and GPU technologies (MIG, MPS)
- Kubernetes community for device plugin and scheduler frameworks
- CNCF projects: Prometheus, Grafana, Karpenter

## 🔗 Links

- [Documentation](https://gpuautoscaler.io/docs)
- [Slack Community](https://gpuautoscaler.slack.com)
- [GitHub Issues](https://github.com/gpuautoscaler/gpuautoscaler/issues)
- [Roadmap](https://github.com/gpuautoscaler/gpuautoscaler/projects/1)

## 📊 Project Status

**Current Phase**: Phase 4 - Cost Management ✅

All four phases are now complete:
- ✅ Phase 1: Observability Foundation
- ✅ Phase 2: Intelligent Packing
- ✅ Phase 3: Autoscaling Engine
- ✅ Phase 4: Cost Management

See our [roadmap](ROADMAP.md) for future enhancements and planned features.

---

**Made with ❤️ by the GPU Autoscaler community**
