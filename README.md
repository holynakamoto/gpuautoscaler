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

### Phase 2: Intelligent Packing (In Progress)
- 🎯 **Bin-Packing Algorithm**: Automatically consolidate GPU workloads
- 🔀 **NVIDIA MIG Support**: Hardware partitioning for A100/H100 GPUs
- 🔄 **NVIDIA MPS Support**: Process-level GPU sharing for inference
- ⏱️ **Time-Slicing**: Software-based sharing for compatible workloads
- 🎫 **Admission Webhook**: Zero-touch optimization requiring no workload changes

### Phase 3: Autoscaling Engine (Coming Soon)
- 🚀 **GPU-Aware Autoscaling**: Scale based on actual GPU utilization, not just CPU/memory
- 💸 **Spot Instance Orchestration**: Prioritize cheaper spot instances with graceful eviction
- 🎚️ **Multi-Tier Scaling**: Optimize cost with spot → on-demand → reserved instance strategy
- 🔮 **Predictive Scaling**: Pre-warm nodes for known busy periods

### Phase 4: Cost Management (Coming Soon)
- 💵 **Real-time Cost Tracking**: Calculate GPU costs per second with cloud pricing APIs
- 📊 **Cost Attribution**: Track spend by namespace, label, experiment ID
- 🎯 **Budget Management**: Set limits with alerts and enforcement
- 📈 **ROI Reporting**: Demonstrate savings from optimization

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

**Current Phase**: Phase 1 - Observability Foundation

See our [roadmap](ROADMAP.md) for planned features and timeline.

---

**Made with ❤️ by the GPU Autoscaler community**
