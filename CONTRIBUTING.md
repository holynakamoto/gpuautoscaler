# Contributing to GPU Autoscaler

Thank you for your interest in contributing to GPU Autoscaler! This document provides guidelines and instructions for contributing.

## Code of Conduct

This project adheres to a Code of Conduct that all contributors are expected to follow. Please be respectful and constructive in all interactions.

## How to Contribute

### Reporting Bugs

If you find a bug, please create an issue with:

1. **Clear title**: Describe the issue concisely
2. **Steps to reproduce**: Detailed steps to reproduce the bug
3. **Expected behavior**: What you expected to happen
4. **Actual behavior**: What actually happened
5. **Environment**: Kubernetes version, GPU types, cluster size
6. **Logs**: Relevant controller/DCGM exporter logs

### Suggesting Features

Feature requests are welcome! Please create an issue with:

1. **Use case**: Describe the problem you're trying to solve
2. **Proposed solution**: Your idea for addressing the use case
3. **Alternatives**: Other solutions you've considered
4. **Impact**: Who would benefit from this feature

### Pull Requests

1. **Fork the repository** and create a branch for your changes
2. **Write code** following our style guidelines (see below)
3. **Add tests** for new functionality
4. **Update documentation** if needed
5. **Run tests** and ensure they pass
6. **Submit PR** with a clear description of changes

#### PR Guidelines

- Keep PRs focused on a single issue or feature
- Write clear commit messages following conventional commits
- Add tests for new functionality
- Ensure all tests pass
- Update documentation as needed
- Respond to review feedback promptly

## Development Setup

### Prerequisites

- Go 1.21+
- Docker
- kubectl
- Kind (for local Kubernetes cluster)
- Helm 3.x

### Local Development

1. **Clone the repository**:
   ```bash
   git clone https://github.com/gpuautoscaler/gpuautoscaler.git
   cd gpuautoscaler
   ```

2. **Install dependencies**:
   ```bash
   make install-deps
   ```

3. **Run tests**:
   ```bash
   make test
   ```

4. **Build the controller**:
   ```bash
   make build
   ```

5. **Build the CLI**:
   ```bash
   make build-cli
   ```

### Testing Locally

#### Option 1: Kind Cluster (without real GPUs)

For testing controller logic without GPUs:

```bash
# Create Kind cluster
kind create cluster --name gpu-autoscaler-dev

# Deploy controller
kubectl apply -f deployments/controller/

# Test with mock metrics
```

#### Option 2: Real GPU Cluster

For full integration testing:

```bash
# Install on your GPU cluster
helm install gpu-autoscaler charts/gpu-autoscaler \
  --namespace gpu-autoscaler-system \
  --create-namespace \
  --values your-values.yaml

# Make changes and upgrade
make docker-build
helm upgrade gpu-autoscaler charts/gpu-autoscaler \
  --set controller.image.tag=your-tag
```

## Code Style

### Go Code

- Follow [Effective Go](https://golang.org/doc/effective_go.html) guidelines
- Use `gofmt` for formatting
- Run `golangci-lint` before committing
- Write godoc comments for exported functions
- Keep functions small and focused

### Commit Messages

Follow [Conventional Commits](https://www.conventionalcommits.org/):

```
<type>(<scope>): <subject>

<body>

<footer>
```

Types:
- `feat`: New feature
- `fix`: Bug fix
- `docs`: Documentation changes
- `style`: Code style changes (formatting, etc.)
- `refactor`: Code refactoring
- `test`: Adding or updating tests
- `chore`: Maintenance tasks

Examples:
```
feat(controller): add MIG support for A100 GPUs

Implement automatic MIG profile configuration based on
workload resource requests.

Closes #123
```

```
fix(metrics): correct VRAM utilization calculation

Previously divided by 1000 instead of 1024 for MB conversion.

Fixes #456
```

## Project Structure

```
gpuautoscaler/
├── cmd/
│   ├── controller/       # Controller entry point
│   └── cli/              # CLI entry point
├── pkg/
│   ├── controller/       # Controller logic
│   ├── metrics/          # Metrics collection
│   ├── packing/          # Bin-packing algorithm
│   ├── cost/             # Cost calculation
│   ├── webhook/          # Admission webhook
│   └── cli/              # CLI commands
├── charts/
│   └── gpu-autoscaler/   # Helm chart
├── deployments/          # Kubernetes manifests
├── docs/                 # Documentation
├── test/                 # Integration tests
└── hack/                 # Development scripts
```

## Testing

### Unit Tests

```bash
make test
```

Write tests for:
- All exported functions
- Edge cases and error handling
- Utility functions

Example:
```go
func TestCalculateWasteScore(t *testing.T) {
    tests := []struct {
        name     string
        gpuUtil  float64
        memUtil  float64
        expected float64
    }{
        {"Full utilization", 100, 100, 0},
        {"Half utilization", 50, 50, 50},
        {"No utilization", 0, 0, 100},
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            result := calculateWasteScore(tt.gpuUtil, tt.memUtil)
            if result != tt.expected {
                t.Errorf("expected %.1f, got %.1f", tt.expected, result)
            }
        })
    }
}
```

### Integration Tests

```bash
make integration-test
```

Integration tests should:
- Use a real or simulated Kubernetes cluster
- Test end-to-end workflows
- Verify metrics collection and analysis

### Linting

```bash
make lint
```

Fix linting issues:
```bash
make lint-fix
```

## Documentation

### Code Documentation

- Add godoc comments to all exported types and functions
- Include examples in documentation
- Keep comments up to date with code changes

### User Documentation

Update relevant docs in `docs/`:
- `installation.md`: Installation instructions
- `configuration.md`: Configuration options
- `user-guide.md`: User-facing features
- `troubleshooting.md`: Common issues
- `api-reference.md`: API documentation

### Architecture Documentation

For significant changes, update:
- `docs/architecture.md`: Architecture overview
- `README.md`: High-level project description

## Release Process

Releases are managed by maintainers. The process is:

1. Update version in `Chart.yaml` and `go.mod`
2. Update `CHANGELOG.md` with release notes
3. Create and push git tag: `git tag -a v0.2.0 -m "Release v0.2.0"`
4. GitHub Actions builds and publishes:
   - Docker images to registry
   - Helm chart to repository
   - CLI binaries to GitHub releases

## Getting Help

- **GitHub Issues**: For bugs and feature requests
- **GitHub Discussions**: For questions and discussions
- **Slack**: Join our [Slack workspace](https://gpuautoscaler.slack.com)
- **Email**: maintainers@gpuautoscaler.io

## Recognition

Contributors are recognized in:
- GitHub contributors list
- Release notes
- Project README

Significant contributions may result in:
- Co-maintainer status
- Speaking opportunities at conferences
- Recognition on project website

## License

By contributing, you agree that your contributions will be licensed under the Apache License 2.0.

---

Thank you for contributing to GPU Autoscaler! 🚀
