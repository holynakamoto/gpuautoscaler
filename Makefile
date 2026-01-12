# GPU Autoscaler Makefile

# Image URL to use all building/pushing image targets
IMG ?= gpuautoscaler/controller:latest
CLI_IMG ?= gpuautoscaler/cli:latest

# ENVTEST_K8S_VERSION refers to the version of kubebuilder assets to be downloaded by envtest binary.
ENVTEST_K8S_VERSION = 1.29.0

# Get the currently used golang install path (in GOPATH/bin, unless GOBIN is set)
ifeq (,$(shell go env GOBIN))
GOBIN=$(shell go env GOPATH)/bin
else
GOBIN=$(shell go env GOBIN)
endif

# CONTAINER_TOOL defines the container tool to be used for building images.
CONTAINER_TOOL ?= docker

# Setting SHELL to bash allows bash commands to be executed by recipes.
SHELL = /usr/bin/env bash -o pipefail
.SHELLFLAGS = -ec

.PHONY: all
all: build

##@ General

.PHONY: help
help: ## Display this help.
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z_0-9-]+:.*?##/ { printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)

##@ Development

.PHONY: manifests
manifests: controller-gen ## Generate WebhookConfiguration, ClusterRole and CustomResourceDefinition objects.
	$(CONTROLLER_GEN) rbac:roleName=manager-role crd webhook paths="./..." output:crd:artifacts:config=config/crd/bases

.PHONY: generate
generate: controller-gen ## Generate code containing DeepCopy, DeepCopyInto, and DeepCopyObject method implementations.
	$(CONTROLLER_GEN) object:headerFile="hack/boilerplate.go.txt" paths="./..."

.PHONY: fmt
fmt: ## Run go fmt against code.
	go fmt ./...

.PHONY: vet
vet: ## Run go vet against code.
	go vet ./...

.PHONY: test
test: manifests generate fmt vet envtest ## Run tests.
	KUBEBUILDER_ASSETS="$(shell $(ENVTEST) use $(ENVTEST_K8S_VERSION) --bin-dir $(LOCALBIN) -p path)" go test ./... -coverprofile cover.out

.PHONY: lint
lint: golangci-lint ## Run golangci-lint linter
	$(GOLANGCI_LINT) run

.PHONY: lint-fix
lint-fix: golangci-lint ## Run golangci-lint linter and perform fixes
	$(GOLANGCI_LINT) run --fix

##@ Build

.PHONY: build
build: manifests generate fmt vet ## Build manager binary.
	go build -o bin/manager cmd/controller/main.go

.PHONY: build-cli
build-cli: fmt vet ## Build CLI binary.
	go build -o bin/gpu-autoscaler cmd/cli/main.go

.PHONY: run
run: manifests generate fmt vet ## Run a controller from your host.
	go run cmd/controller/main.go

# Build the docker image for the controller
.PHONY: docker-build
docker-build: ## Build docker image with the manager.
	$(CONTAINER_TOOL) build -t ${IMG} -f deployments/controller/Dockerfile .

# Push the docker image for the controller
.PHONY: docker-push
docker-push: ## Push docker image with the manager.
	$(CONTAINER_TOOL) push ${IMG}

# Build the docker image for the CLI
.PHONY: docker-build-cli
docker-build-cli: ## Build docker image with the CLI.
	$(CONTAINER_TOOL) build -t ${CLI_IMG} -f deployments/cli/Dockerfile .

# Push the docker image for the CLI
.PHONY: docker-push-cli
docker-push-cli: ## Push docker image with the CLI.
	$(CONTAINER_TOOL) push ${CLI_IMG}

##@ Deployment

ifndef ignore-not-found
  ignore-not-found = false
endif

.PHONY: install
install: manifests kustomize ## Install CRDs into the K8s cluster specified in ~/.kube/config.
	$(KUSTOMIZE) build config/crd | $(KUBECTL) apply -f -

.PHONY: uninstall
uninstall: manifests kustomize ## Uninstall CRDs from the K8s cluster specified in ~/.kube/config. Call with ignore-not-found=true to ignore resource not found errors during deletion.
	$(KUSTOMIZE) build config/crd | $(KUBECTL) delete --ignore-not-found=$(ignore-not-found) -f -

.PHONY: deploy
deploy: manifests kustomize ## Deploy controller to the K8s cluster specified in ~/.kube/config.
	cd config/manager && $(KUSTOMIZE) edit set image controller=${IMG}
	$(KUSTOMIZE) build config/default | $(KUBECTL) apply -f -

.PHONY: undeploy
undeploy: kustomize ## Undeploy controller from the K8s cluster specified in ~/.kube/config. Call with ignore-not-found=true to ignore resource not found errors during deletion.
	$(KUSTOMIZE) build config/default | $(KUBECTL) delete --ignore-not-found=$(ignore-not-found) -f -

##@ Dependencies

## Location to install dependencies to
LOCALBIN ?= $(shell pwd)/bin
$(LOCALBIN):
	mkdir -p $(LOCALBIN)

## Tool Binaries
KUBECTL ?= kubectl
KUSTOMIZE ?= $(LOCALBIN)/kustomize
CONTROLLER_GEN ?= $(LOCALBIN)/controller-gen
ENVTEST ?= $(LOCALBIN)/setup-envtest
GOLANGCI_LINT ?= $(LOCALBIN)/golangci-lint

## Tool Versions
KUSTOMIZE_VERSION ?= v5.3.0
CONTROLLER_TOOLS_VERSION ?= v0.14.0
GOLANGCI_LINT_VERSION ?= v1.55.2

.PHONY: kustomize
kustomize: $(KUSTOMIZE) ## Download kustomize locally if necessary.
$(KUSTOMIZE): $(LOCALBIN)
	$(call go-install-tool,$(KUSTOMIZE),sigs.k8s.io/kustomize/kustomize/v5,$(KUSTOMIZE_VERSION))

.PHONY: controller-gen
controller-gen: $(CONTROLLER_GEN) ## Download controller-gen locally if necessary.
$(CONTROLLER_GEN): $(LOCALBIN)
	$(call go-install-tool,$(CONTROLLER_GEN),sigs.k8s.io/controller-tools/cmd/controller-gen,$(CONTROLLER_TOOLS_VERSION))

.PHONY: envtest
envtest: $(ENVTEST) ## Download setup-envtest locally if necessary.
$(ENVTEST): $(LOCALBIN)
	$(call go-install-tool,$(ENVTEST),sigs.k8s.io/controller-runtime/tools/setup-envtest,latest)

.PHONY: golangci-lint
golangci-lint: $(GOLANGCI_LINT) ## Download golangci-lint locally if necessary.
$(GOLANGCI_LINT): $(LOCALBIN)
	$(call go-install-tool,$(GOLANGCI_LINT),github.com/golangci/golangci-lint/cmd/golangci-lint,${GOLANGCI_LINT_VERSION})

# go-install-tool will 'go install' any package with custom target and name of binary, if it doesn't exist
# $1 - target path with name of binary (ideally with version)
# $2 - package url which can be installed
# $3 - specific version of package
define go-install-tool
@[ -f $(1) ] || { \
set -e; \
package=$(2)@$(3) ;\
echo "Downloading $${package}" ;\
GOBIN=$(LOCALBIN) go install $${package} ;\
mv "$$(echo "$(1)" | sed "s/-$$($(3))$$//")" $(1) || true ;\
}
endef

.PHONY: install-deps
install-deps: kustomize controller-gen envtest golangci-lint ## Install all development dependencies

.PHONY: dev-cluster
dev-cluster: ## Create a local Kind cluster with GPU support for development
	@echo "Note: This requires Docker and Kind installed, plus NVIDIA Container Toolkit"
	@echo "Creating Kind cluster with GPU support..."
	kind create cluster --config=hack/kind-gpu-config.yaml --name=gpu-autoscaler-dev || true
	kubectl config use-context kind-gpu-autoscaler-dev

.PHONY: dev-cluster-delete
dev-cluster-delete: ## Delete the local Kind development cluster
	kind delete cluster --name=gpu-autoscaler-dev

##@ Helm

.PHONY: helm-lint
helm-lint: ## Lint Helm charts
	helm lint charts/gpu-autoscaler

.PHONY: helm-package
helm-package: ## Package Helm chart
	helm package charts/gpu-autoscaler -d dist/

.PHONY: helm-install
helm-install: ## Install Helm chart to cluster
	helm install gpu-autoscaler charts/gpu-autoscaler \
		--namespace gpu-autoscaler-system \
		--create-namespace

.PHONY: helm-uninstall
helm-uninstall: ## Uninstall Helm chart from cluster
	helm uninstall gpu-autoscaler --namespace gpu-autoscaler-system

.PHONY: helm-upgrade
helm-upgrade: ## Upgrade Helm chart in cluster
	helm upgrade gpu-autoscaler charts/gpu-autoscaler \
		--namespace gpu-autoscaler-system

##@ Release

.PHONY: release-build
release-build: ## Build release binaries for multiple platforms
	@echo "Building release binaries..."
	GOOS=linux GOARCH=amd64 go build -o dist/gpu-autoscaler-linux-amd64 cmd/cli/main.go
	GOOS=linux GOARCH=arm64 go build -o dist/gpu-autoscaler-linux-arm64 cmd/cli/main.go
	GOOS=darwin GOARCH=amd64 go build -o dist/gpu-autoscaler-darwin-amd64 cmd/cli/main.go
	GOOS=darwin GOARCH=arm64 go build -o dist/gpu-autoscaler-darwin-arm64 cmd/cli/main.go

.PHONY: clean
clean: ## Clean build artifacts
	rm -rf bin/ dist/ cover.out
