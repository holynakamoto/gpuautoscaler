# Installation Guide

This guide walks you through installing GPU Autoscaler on your Kubernetes cluster.

## Prerequisites

Before installing GPU Autoscaler, ensure your cluster meets these requirements:

### Required

- **Kubernetes**: Version 1.25 or later
- **NVIDIA GPU Nodes**: At least one node with NVIDIA GPUs
- **NVIDIA GPU Drivers**: Version 470.x or later installed on GPU nodes
- **NVIDIA Device Plugin**: Installed and running (for GPU resource allocation)
- **Helm**: Version 3.x for installation
- **kubectl**: Configured to access your cluster

### Recommended

- **Prometheus Operator**: For easier metrics collection (can be installed by the chart)
- **cert-manager**: For automatic TLS certificate management for webhooks
- **Persistent Storage**: For Prometheus and Grafana data retention

## Quick Start

### 1. Install NVIDIA Device Plugin (if not already installed)

```bash
kubectl apply -f https://raw.githubusercontent.com/NVIDIA/k8s-device-plugin/v0.14.0/nvidia-device-plugin.yml
```

Verify GPUs are detected:

```bash
kubectl get nodes -o json | jq '.items[].status.capacity."nvidia.com/gpu"'
```

### 2. Install GPU Autoscaler

Add the Helm repository:

```bash
helm repo add gpu-autoscaler https://gpuautoscaler.github.io/charts
helm repo update
```

Install with default settings:

```bash
helm install gpu-autoscaler gpu-autoscaler/gpu-autoscaler \
  --namespace gpu-autoscaler-system \
  --create-namespace
```

### 3. Verify Installation

Check all pods are running:

```bash
kubectl get pods -n gpu-autoscaler-system
```

Expected output:

```
NAME                                        READY   STATUS    RESTARTS   AGE
gpu-autoscaler-controller-xxxxx             1/1     Running   0          2m
dcgm-exporter-xxxxx                         1/1     Running   0          2m
prometheus-operated-0                       1/1     Running   0          2m
grafana-xxxxx                              1/1     Running   0          2m
```

### 4. Access Grafana Dashboard

Port-forward Grafana:

```bash
kubectl port-forward -n gpu-autoscaler-system svc/grafana 3000:80
```

Open http://localhost:3000 in your browser.

Default credentials:
- Username: `admin`
- Password: Get from secret: `kubectl get secret -n gpu-autoscaler-system grafana -o jsonpath="{.data.admin-password}" | base64 --decode`

### 5. Install CLI Tool

Download and install the CLI:

```bash
curl -sSL https://gpuautoscaler.io/install.sh | bash
```

Or manually:

```bash
# Linux
curl -LO https://github.com/gpuautoscaler/gpuautoscaler/releases/download/v0.1.0/gpu-autoscaler-linux-amd64
chmod +x gpu-autoscaler-linux-amd64
sudo mv gpu-autoscaler-linux-amd64 /usr/local/bin/gpu-autoscaler

# macOS
curl -LO https://github.com/gpuautoscaler/gpuautoscaler/releases/download/v0.1.0/gpu-autoscaler-darwin-amd64
chmod +x gpu-autoscaler-darwin-amd64
sudo mv gpu-autoscaler-darwin-amd64 /usr/local/bin/gpu-autoscaler
```

Verify CLI installation:

```bash
gpu-autoscaler status
```

## Advanced Installation

### Custom Values

Create a `values.yaml` file with your customizations:

```yaml
# Enable admission webhook for automatic optimization
admissionWebhook:
  enabled: true
  rewriteRequests: true

# Enable GPU sharing via MIG
sharing:
  mig:
    enabled: true
    autoConfig: true

# Enable autoscaling
autoscaling:
  enabled: true
  spot:
    enabled: true
    targetPercentage: 60

# Prometheus retention
prometheus:
  server:
    retention: "14d"
    persistentVolume:
      size: 100Gi

# Grafana configuration
grafana:
  adminPassword: "your-secure-password"
  persistence:
    size: 20Gi
```

Install with custom values:

```bash
helm install gpu-autoscaler gpu-autoscaler/gpu-autoscaler \
  --namespace gpu-autoscaler-system \
  --create-namespace \
  --values values.yaml
```

### Install cert-manager (for webhook TLS)

If you don't have cert-manager installed:

```bash
kubectl apply -f https://github.com/cert-manager/cert-manager/releases/download/v1.13.0/cert-manager.yaml
```

GPU Autoscaler will automatically create certificates for the webhook server.

### Cloud Provider Configuration

#### AWS

For autoscaling on AWS, the controller needs IAM permissions:

```yaml
# IAM Policy
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Effect": "Allow",
      "Action": [
        "autoscaling:DescribeAutoScalingGroups",
        "autoscaling:SetDesiredCapacity",
        "ec2:DescribeInstances",
        "ec2:DescribeSpotInstanceRequests",
        "pricing:GetProducts"
      ],
      "Resource": "*"
    }
  ]
}
```

Install with AWS configuration:

```bash
helm install gpu-autoscaler gpu-autoscaler/gpu-autoscaler \
  --namespace gpu-autoscaler-system \
  --create-namespace \
  --set autoscaling.enabled=true \
  --set autoscaling.spot.enabled=true \
  --set-string controller.annotations."iam\.amazonaws\.com/role"="gpu-autoscaler-role"
```

#### GCP

For GCP, use Workload Identity:

```bash
# Create service account
gcloud iam service-accounts create gpu-autoscaler

# Grant permissions
gcloud projects add-iam-policy-binding PROJECT_ID \
  --member="serviceAccount:gpu-autoscaler@PROJECT_ID.iam.gserviceaccount.com" \
  --role="roles/compute.instanceAdmin.v1"

# Bind to Kubernetes service account
gcloud iam service-accounts add-iam-policy-binding \
  gpu-autoscaler@PROJECT_ID.iam.gserviceaccount.com \
  --role roles/iam.workloadIdentityUser \
  --member "serviceAccount:PROJECT_ID.svc.id.goog[gpu-autoscaler-system/gpu-autoscaler]"

# Install with Workload Identity
helm install gpu-autoscaler gpu-autoscaler/gpu-autoscaler \
  --namespace gpu-autoscaler-system \
  --create-namespace \
  --set serviceAccount.annotations."iam\.gke\.io/gcp-service-account"="gpu-autoscaler@PROJECT_ID.iam.gserviceaccount.com"
```

## Upgrading

### Upgrade to Latest Version

```bash
helm repo update
helm upgrade gpu-autoscaler gpu-autoscaler/gpu-autoscaler \
  --namespace gpu-autoscaler-system
```

### Upgrade with Custom Values

```bash
helm upgrade gpu-autoscaler gpu-autoscaler/gpu-autoscaler \
  --namespace gpu-autoscaler-system \
  --values values.yaml
```

## Uninstalling

To completely remove GPU Autoscaler:

```bash
helm uninstall gpu-autoscaler --namespace gpu-autoscaler-system
kubectl delete namespace gpu-autoscaler-system
```

## Troubleshooting

### DCGM Exporter Not Starting

Check if DCGM is running on nodes:

```bash
kubectl logs -n gpu-autoscaler-system daemonset/dcgm-exporter
```

Common issues:
- NVIDIA drivers not installed
- DCGM already running (conflict)
- Insufficient permissions

### Controller Not Starting

Check controller logs:

```bash
kubectl logs -n gpu-autoscaler-system deployment/gpu-autoscaler-controller
```

Common issues:
- Cannot connect to Prometheus
- RBAC permissions missing
- Invalid configuration

### No Metrics in Grafana

1. Check Prometheus is scraping DCGM exporter:
   ```bash
   kubectl port-forward -n gpu-autoscaler-system svc/prometheus-operated 9090:9090
   ```
   Open http://localhost:9090 and query: `DCGM_FI_DEV_GPU_UTIL`

2. Check ServiceMonitor is created:
   ```bash
   kubectl get servicemonitor -n gpu-autoscaler-system
   ```

3. Check Grafana datasource configuration in the Grafana UI

## Next Steps

- [Configure GPU Sharing](./configuration.md#gpu-sharing)
- [Enable Autoscaling](./configuration.md#autoscaling)
- [Set Up Cost Tracking](./configuration.md#cost-tracking)
- [View User Guide](./user-guide.md)
