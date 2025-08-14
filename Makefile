#
# Copyright (c) 2023 Red Hat, Inc.
#
# Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file except
# in compliance with the License. You may obtain a copy of the License at
#
#   http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software distributed under the License
# is distributed on an "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express
# or implied. See the License for the specific language governing permissions and limitations under
# the License.
#

# Additional flags to pass to the `ginkgo` command.
ginkgo_flags:=

# VERSION defines the project version for the bundle.
# Update this value when you upgrade the version of your project.
# To re-generate a bundle for another specific version without changing the standard setup, you can:
# - use the VERSION as arg of the bundle target (e.g make bundle VERSION=0.0.2)
# - use environment variables to overwrite this value (e.g export VERSION=0.0.2)
VERSION ?= 4.20.0

PACKAGE_NAME ?= oran-o2ims

# OPERATOR_SDK_VERSION defines the operator-sdk version to download from GitHub releases.
OPERATOR_SDK_VERSION ?= v1.41.1

# YQ_VERSION defines the yq version to download from GitHub releases.
YQ_VERSION ?= v4.45.4

# OPM_VERSION defines the opm version to download from GitHub releases.
OPM_VERSION ?= v1.52.0

# GOLANGCI_LINT_VERSION the opm version to download from GitHub releases.
GOLANGCI_LINT_VERSION ?= v2.3.0

PACKAGE_NAME_KONFLUX = o-cloud-manager
CATALOG_TEMPLATE_KONFLUX_INPUT = .konflux/catalog/catalog-template.in.yaml
CATALOG_TEMPLATE_KONFLUX_OUTPUT = .konflux/catalog/catalog-template.out.yaml
CATALOG_KONFLUX = .konflux/catalog/$(PACKAGE_NAME_KONFLUX)/catalog.yaml

# Konflux bundle image configuration
BUNDLE_NAME_SUFFIX = bundle-4-20
PRODUCTION_BUNDLE_NAME = operator-bundle

PROJECT_DIR := $(shell dirname $(abspath $(firstword $(MAKEFILE_LIST))))

# Development/Debug passwords for database.  This requires that the operator be deployed in DEBUG=yes mode or for the
# developer to override these values with the current passwords
ORAN_O2IMS_ALARMS_PASSWORD ?= debug
ORAN_O2IMS_RESOURCES_PASSWORD ?= debug

ifeq (${DEBUG}, yes)
	DOCKER_TARGET = debug
	GOBUILD_GCFLAGS = all=-N -l
	KUSTOMIZE_OVERLAY = debug
else
	DOCKER_TARGET = production
	GOBUILD_GCFLAGS = ""
	KUSTOMIZE_OVERLAY = default
endif

# DEFAULT_CHANNEL defines the default channel used in the bundle.
# Add a new line here if you would like to change its default config. (E.g DEFAULT_CHANNEL = "stable")
# To re-generate a bundle for any other default channel without changing the default setup, you can:
# - use the DEFAULT_CHANNEL as arg of the bundle target (e.g make bundle DEFAULT_CHANNEL=stable)
# - use environment variables to overwrite this value (e.g export DEFAULT_CHANNEL="stable")
ifneq ($(origin DEFAULT_CHANNEL), undefined)
BUNDLE_DEFAULT_CHANNEL := --default-channel=$(DEFAULT_CHANNEL)
endif
BUNDLE_METADATA_OPTS ?= $(BUNDLE_CHANNELS) $(BUNDLE_DEFAULT_CHANNEL)

# IMAGE_TAG_BASE defines the docker.io namespace and part of the image name for remote images.
# This variable is used to construct full image tags for bundle and catalog images.
#
# For example, running 'make bundle-build bundle-push catalog-build catalog-push' will build and push both
# openshift.io/oran-o2ims-bundle:$VERSION and openshift.io/oran-o2ims-catalog:$VERSION.
IMAGE_NAME ?= oran-o2ims-operator
IMAGE_TAG_BASE ?= quay.io/openshift-kni/${IMAGE_NAME}

# BUNDLE_IMG defines the image:tag used for the bundle.
# You can use it as an arg. (E.g make bundle-build BUNDLE_IMG=<some-registry>/<project-name-bundle>:<tag>)
BUNDLE_IMG ?= $(IMAGE_TAG_BASE)-bundle:$(VERSION)

# BUNDLE_GEN_FLAGS are the flags passed to the operator-sdk generate bundle command
BUNDLE_GEN_FLAGS ?= -q --overwrite --version $(VERSION) $(BUNDLE_METADATA_OPTS)

# USE_IMAGE_DIGESTS defines if images are resolved via tags or digests
# You can enable this value if you would like to use SHA Based Digests
# To enable set flag to true
USE_IMAGE_DIGESTS ?= false
ifeq ($(USE_IMAGE_DIGESTS), true)
        BUNDLE_GEN_FLAGS += --use-image-digests
endif

# Image URL to use all building/pushing image targets
IMG ?= $(IMAGE_TAG_BASE):$(VERSION)

# ENVTEST_K8S_VERSION refers to the version of kubebuilder assets to be downloaded by envtest binary.
ENVTEST_K8S_VERSION = 1.32.0
ENVTEST_VERSION = release-0.21

# OCLOUD_MANAGER_NAMESPACE refers to the namespace of the O-Cloud Manager
OCLOUD_MANAGER_NAMESPACE ?= oran-o2ims

# HWMGR_PLUGIN_NAMESPACE refers to the namespace of the hardware manager plugin.
HWMGR_PLUGIN_NAMESPACE ?= oran-o2ims

# Get the currently used golang install path (in GOPATH/bin, unless GOBIN is set)
ifeq (,$(shell go env GOBIN))
GOBIN=$(shell go env GOPATH)/bin
else
GOBIN=$(shell go env GOBIN)
endif

# IMAGE_PULL_POLICY sets the value that is patched into the CSV for the manager container imagePullPolicy.
# If the IMAGE_TAG_BASE is a user repo, the default value is updated to imagePullPolicy=Always.
ifneq (,$(findstring openshift-kni,$(IMAGE_TAG_BASE)))
IMAGE_PULL_POLICY ?= IfNotPresent
else
IMAGE_PULL_POLICY ?= Always
endif

# CONTAINER_TOOL defines the container tool to be used for building images.
# Be aware that the target commands are only tested with Docker which is
# scaffolded by default. However, you might want to replace it to use other
# tools. (i.e. podman)
CONTAINER_TOOL ?= docker

# Set SKIP_SUBMODULE_SYNC to yes to avoid running the `git submodule update` command in update_deps.sh
export SKIP_SUBMODULE_SYNC ?= no

# Setting SHELL to bash allows bash commands to be executed by recipes.
# Options are set to exit when a recipe line exits non-zero or a piped command fails.
SHELL = /usr/bin/env bash -o pipefail
.SHELLFLAGS = -ec

# This allows all tools under '$(PWD)/bin', ie:opm,yq ... To be used by targets containing scripts
export PATH  := $(PATH):$(PWD)/bin

# Source directories
SOURCE_DIRS := $(shell find . -maxdepth 1 -type d ! -name "vendor" ! -name "." ! -name ".*")

.PHONY: all
all: build

#@ General

# The help target prints out all targets with their descriptions organized
# beneath their categories. The categories are represented by '##@' and the
# target descriptions by '##'. The awk command is responsible for reading the
# entire set of makefiles included in this invocation, looking for lines of the
# file as xyz: ## something, and then pretty-format the target and help. Then,
# if there's a line with ##@ something, that gets pretty-printed as a category.
# More info on the usage of ANSI control characters for terminal formatting:
# https://en.wikipedia.org/wiki/ANSI_escape_code#SGR_parameters
# More info on the awk command:
# http://linuxcommand.org/lc3_adv_awk.php

.PHONY: help
help: ## Display this help.
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z_0-9-]+:.*?##/ { printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)

##@ Development

.PHONY: manifests
manifests: deps-update controller-gen ## Generate WebhookConfiguration, ClusterRole and CustomResourceDefinition objects.
	$(CONTROLLER_GEN) rbac:roleName=manager-role crd webhook paths="./..." output:crd:artifacts:config=config/crd/bases

.PHONY: generate
generate: deps-update controller-gen ## Generate code containing DeepCopy, DeepCopyInto, and DeepCopyObject method implementations.
	$(CONTROLLER_GEN) object:headerFile="hack/boilerplate.go.txt" paths="./..."

##@ Build
.PHONY: build
build: manifests generate fmt vet ## Build manager binary.
	go build -gcflags "${GOBUILD_GCFLAGS}"

.PHONY: run
run: manifests generate fmt vet binary ## Run a controller from your host.
	IMAGE=$(IMAGE_TAG_BASE):$(VERSION) $(LOCALBIN)/$(BINARY_NAME) start controller-manager --enable-webhooks=false

# If you wish to build the manager image targeting other platforms you can use the --platform flag.
# (i.e. docker build --platform linux/arm64). However, you must enable docker buildKit for it.
# More info: https://docs.docker.com/develop/develop-images/build_enhancements/

PLATFORM ?= linux/amd64
.PHONY: docker-build
docker-build: manifests generate fmt vet ## Build docker image with the manager.
	$(CONTAINER_TOOL) build -t ${IMG} -f Dockerfile --platform=$(PLATFORM) --target ${DOCKER_TARGET} --build-arg "GOBUILD_GCFLAGS=${GOBUILD_GCFLAGS}" .

.PHONY: docker-push
docker-push: docker-build ## Push docker image with the manager.
	$(CONTAINER_TOOL) push ${IMG}

# PLATFORMS defines the target platforms for the manager image be built to provide support to multiple
# architectures. (i.e. make docker-buildx IMG=myregistry/mypoperator:0.0.1). To use this option you need to:
# - be able to use docker buildx. More info: https://docs.docker.com/build/buildx/
# - have enabled BuildKit. More info: https://docs.docker.com/develop/develop-images/build_enhancements/
# - be able to push the image to your registry (i.e. if you do not set a valid value via IMG=<myregistry/image:<tag>> then the export will fail)
# To adequately provide solutions that are compatible with multiple platforms, you should consider using this option.
PLATFORMS ?= linux/arm64,linux/amd64,linux/s390x,linux/ppc64le
.PHONY: docker-buildx
docker-buildx: ## Build and push docker image for the manager for cross-platform support
	# copy existing Dockerfile and insert --platform=${BUILDPLATFORM} into Dockerfile.cross, and preserve the original Dockerfile
	sed -e '1 s/\(^FROM\)/FROM --platform=\$$\{BUILDPLATFORM\}/; t' -e ' 1,// s//FROM --platform=\$$\{BUILDPLATFORM\}/' Dockerfile > Dockerfile.cross
	- $(CONTAINER_TOOL) buildx create --name project-v3-builder
	$(CONTAINER_TOOL) buildx use project-v3-builder
	- $(CONTAINER_TOOL) buildx build --push --platform=$(PLATFORMS) --tag ${IMG} -f Dockerfile.cross .
	- $(CONTAINER_TOOL) buildx rm project-v3-builder
	rm Dockerfile.cross

##@ Deployment

ifndef ignore-not-found
  ignore-not-found = false
endif

.PHONY: install
install: manifests kustomize kubectl ## Install CRDs into the K8s cluster specified in ~/.kube/config.
	$(KUSTOMIZE) build config/crd | $(KUBECTL) apply -f -

.PHONY: uninstall
uninstall: manifests kustomize kubectl ## Uninstall CRDs from the K8s cluster specified in ~/.kube/config. Call with ignore-not-found=true to ignore resource not found errors during deletion.
	$(KUSTOMIZE) build config/crd | $(KUBECTL) delete --ignore-not-found=$(ignore-not-found) -f -

.PHONY: deploy
deploy: install manifests kustomize kubectl ## Deploy controller to the K8s cluster specified in ~/.kube/config.
	@$(KUBECTL) create configmap env-config \
		--from-literal=HWMGR_PLUGIN_NAMESPACE=$(HWMGR_PLUGIN_NAMESPACE) \
		--from-literal=imagePullPolicy=$(IMAGE_PULL_POLICY) \
		--dry-run=client -o yaml > config/manager/env-config.yaml
	cd config/manager \
		&& $(KUSTOMIZE) edit set image controller=${IMG}
	$(KUSTOMIZE) build config/$(KUSTOMIZE_OVERLAY) | $(KUBECTL) apply -f -

.PHONY: undeploy
undeploy: kustomize kubectl ## Undeploy controller from the K8s cluster specified in ~/.kube/config. Call with ignore-not-found=true to ignore resource not found errors during deletion.
	$(KUSTOMIZE) build config/$(KUSTOMIZE_OVERLAY) | $(KUBECTL) delete --ignore-not-found=$(ignore-not-found) -f -

##@ Build Dependencies

## oran-binary
BINARY_NAME := oran-o2ims

## Location to install dependencies to
LOCALBIN ?= $(shell pwd)/bin
$(LOCALBIN):
	mkdir -p $(LOCALBIN)

## Tool Binaries
KUBECTL ?= $(LOCALBIN)/kubectl
KUSTOMIZE ?= $(LOCALBIN)/kustomize
CONTROLLER_GEN ?= $(LOCALBIN)/controller-gen
ENVTEST ?= $(LOCALBIN)/setup-envtest
OPERATOR_SDK ?= $(LOCALBIN)/operator-sdk
OPM ?= $(LOCALBIN)/opm
YQ ?= $(LOCALBIN)/yq

## Tool Versions
KUSTOMIZE_VERSION ?= v5.7.1
CONTROLLER_TOOLS_VERSION ?= v0.18.0

.PHONY: kubectl
kubectl: $(KUBECTL) ## Use envtest to download kubectl
$(KUBECTL): $(LOCALBIN) envtest
	if [ ! -f $(KUBECTL) ]; then \
		KUBEBUILDER_ASSETS=$$($(ENVTEST) use $(ENVTEST_K8S_VERSION) --bin-dir $(LOCALBIN) -p path); \
		ln -sf $${KUBEBUILDER_ASSETS}/kubectl $(KUBECTL); \
	fi

.PHONY: kustomize
kustomize: $(KUSTOMIZE) ## Download kustomize locally if necessary. If wrong version is installed, it will be removed before downloading.
$(KUSTOMIZE): $(LOCALBIN)
	@if test -x $(LOCALBIN)/kustomize && ! $(LOCALBIN)/kustomize version | grep -q $(KUSTOMIZE_VERSION); then \
		echo "$(LOCALBIN)/kustomize version is not expected $(KUSTOMIZE_VERSION). Removing it before installing."; \
		rm -rf $(LOCALBIN)/kustomize; \
	fi
	test -s $(LOCALBIN)/kustomize || GOBIN=$(LOCALBIN) GO111MODULE=on go install sigs.k8s.io/kustomize/kustomize/v5@$(KUSTOMIZE_VERSION)

.PHONY: controller-gen
controller-gen: $(CONTROLLER_GEN) ## Download controller-gen locally if necessary. If wrong version is installed, it will be overwritten.
$(CONTROLLER_GEN): $(LOCALBIN)
	test -s $(LOCALBIN)/controller-gen && $(LOCALBIN)/controller-gen --version | grep -q $(CONTROLLER_TOOLS_VERSION) || \
	GOBIN=$(LOCALBIN) go install sigs.k8s.io/controller-tools/cmd/controller-gen@$(CONTROLLER_TOOLS_VERSION)

.PHONY: envtest
envtest: $(ENVTEST) ## Download envtest-setup locally if necessary.
$(ENVTEST): $(LOCALBIN)
	@chmod u+w $(LOCALBIN)
	test -s $(LOCALBIN)/setup-envtest || GOBIN=$(LOCALBIN) go install sigs.k8s.io/controller-runtime/tools/setup-envtest@$(ENVTEST_VERSION)

# Determine sed flags based on the operating system
ifeq ($(shell uname -s),Linux)
SED_FLAGS := -i
else
SED_FLAGS := -i ''
endif

.PHONY: bundle
bundle: operator-sdk manifests kustomize kubectl ## Generate bundle manifests and metadata, then validate generated files.
	$(OPERATOR_SDK) generate kustomize manifests --apis-dir api/ -q
	@$(KUBECTL) create configmap env-config \
		--from-literal=HWMGR_PLUGIN_NAMESPACE=$(HWMGR_PLUGIN_NAMESPACE) \
		--from-literal=imagePullPolicy=$(IMAGE_PULL_POLICY) \
		--dry-run=client -o yaml > config/manager/env-config.yaml
	cd config/manager \
		&& $(KUSTOMIZE) edit set image controller=$(IMG)
	$(KUSTOMIZE) build config/manifests | $(OPERATOR_SDK) generate bundle $(BUNDLE_GEN_FLAGS)
	@rm bundle/manifests/oran-o2ims-env-config_v1_configmap.yaml ## Clean up the temporary file for bundle validate
	$(OPERATOR_SDK) bundle validate ./bundle
	sed $(SED_FLAGS) -e '/^[[:space:]]*createdAt:/d' bundle/manifests/oran-o2ims.clusterserviceversion.yaml

.PHONY: bundle-build
bundle-build: bundle docker-push ## Build the bundle image.
	$(CONTAINER_TOOL) build -f bundle.Dockerfile -t $(BUNDLE_IMG) .

.PHONY: bundle-push
bundle-push: bundle-build ## Push the bundle image.
	$(CONTAINER_TOOL) push $(BUNDLE_IMG)

.PHONY: bundle-check
bundle-check: bundle
	hack/check-git-tree.sh

.PHONY: bundle-run
bundle-run: # Install bundle on cluster using operator sdk.
	oc create ns $(OCLOUD_MANAGER_NAMESPACE)
	$(OPERATOR_SDK) --security-context-config restricted -n $(OCLOUD_MANAGER_NAMESPACE) run bundle $(BUNDLE_IMG)

.PHONY: bundle-upgrade
bundle-upgrade: # Upgrade bundle on cluster using operator sdk.
	$(OPERATOR_SDK) run bundle-upgrade $(BUNDLE_IMG)

.PHONY: bundle-clean
bundle-clean: # Uninstall bundle on cluster using operator sdk.
	$(OPERATOR_SDK) cleanup $(PACKAGE_NAME) -n $(OCLOUD_MANAGER_NAMESPACE)
	oc delete ns $(OCLOUD_MANAGER_NAMESPACE)

# A comma-separated list of bundle images (e.g. make catalog-build BUNDLE_IMGS=example.com/operator-bundle:v0.1.0,example.com/operator-bundle:v0.2.0).
# These images MUST exist in a registry and be pull-able.
BUNDLE_IMGS ?= $(BUNDLE_IMG)

# The image tag given to the resulting catalog image (e.g. make catalog-build CATALOG_IMG=example.com/operator-catalog:v0.2.0).
CATALOG_IMG ?= $(IMAGE_TAG_BASE)-catalog:v$(VERSION)

# Set CATALOG_BASE_IMG to an existing catalog image tag to add $BUNDLE_IMGS to that image.
ifneq ($(origin CATALOG_BASE_IMG), undefined)
FROM_INDEX_OPT := --from-index $(CATALOG_BASE_IMG)
endif

# Build a catalog image by adding bundle images to an empty catalog using the operator package manager tool, 'opm'.
# This recipe invokes 'opm' in 'semver' bundle add mode. For more information on add modes, see:
# https://github.com/operator-framework/community-operators/blob/7f1438c/docs/packaging-operator.md#updating-your-existing-operator
.PHONY: catalog
catalog: opm ## Build a catalog.
	@mkdir -p catalog
	hack/generate-catalog-index.sh --opm $(OPM) --name $(PACKAGE_NAME) --channel alpha --version $(VERSION)
	$(OPM) render --output=yaml $(BUNDLE_IMG) > catalog/$(PACKAGE_NAME).yaml
	$(OPM) validate catalog

.PHONY: catalog-build
catalog-build: catalog ## Build a catalog image.
	$(CONTAINER_TOOL) build -f catalog.Dockerfile -t $(CATALOG_IMG) .

# Push the catalog image.
.PHONY: catalog-push
catalog-push: ## Push a catalog image.
	$(CONTAINER_TOOL) push $(CATALOG_IMG)

# Deploy from catalog image.
.PHONY: catalog-deploy
catalog-deploy: ## Deploy from catalog image.
	hack/generate-catalog-deploy.sh \
		--package $(PACKAGE_NAME) \
		--namespace $(OCLOUD_MANAGER_NAMESPACE) \
		--catalog-image $(CATALOG_IMG) \
		--channel alpha \
		--install-mode OwnNamespace \
		| oc create -f -

# Undeploy from catalog image.
.PHONY: catalog-undeploy
catalog-undeploy: ## Undeploy from catalog image.
	hack/catalog-undeploy.sh --package $(PACKAGE_NAME) --namespace $(OCLOUD_MANAGER_NAMESPACE) --crd-search "o2ims.*oran"

##@ Tools and Linting

.PHONY: lint
lint: golangci-lint yamllint

.PHONY: golangci-lint
golangci-lint: ## Run golangci-lint against code.
	@echo "Downloading golangci-lint..."
	$(MAKE) -C $(PROJECT_DIR)/telco5g-konflux/scripts/download download-golangci-lint \
		DOWNLOAD_INSTALL_DIR=$(PROJECT_DIR)/bin \
		DOWNLOAD_GOLANGCI_LINT_VERSION=$(GOLANGCI_LINT_VERSION)
	@echo "Golangci-lint downloaded successfully."
	@echo "Running golangci-lint on repository go files..."
	golangci-lint --version
	golangci-lint run -v
	@echo "Golangci-lint linting completed successfully."

.PHONY: tools
tools: opm operator-sdk yq

.PHONY: bashate
bashate: ## Download bashate and lint bash files in the repository
	@echo "Downloading bashate..."
	$(MAKE) -C $(PROJECT_DIR)/telco5g-konflux/scripts/download download-bashate DOWNLOAD_INSTALL_DIR=$(PROJECT_DIR)/bin
	@echo "Bashate downloaded successfully."
	@echo "Running bashate on repository bash files..."
	find . -name '*.sh' \
		-not -path './vendor/*' \
		-not -path './*/vendor/*' \
		-not -path './git/*' \
		-not -path './bin/*' \
		-not -path './testbin/*' \
		-not -path './telco5g-konflux/*' \
		-print0 \
		| xargs -0 --no-run-if-empty bashate -v -e 'E*' -i E006
	@echo "Bashate linting completed successfully."

operator-sdk: ## Download operator-sdk locally if necessary
	@$(MAKE) -C $(PROJECT_DIR)/telco5g-konflux/scripts/download download-operator-sdk \
		DOWNLOAD_INSTALL_DIR=$(PROJECT_DIR)/bin \
		DOWNLOAD_OPERATOR_SDK_VERSION=$(OPERATOR_SDK_VERSION)
	@echo "Operator sdk downloaded successfully."

.PHONY: opm
opm: ## Download opm locally if necessary
	@$(MAKE) -C $(PROJECT_DIR)/telco5g-konflux/scripts/download download-opm \
		DOWNLOAD_INSTALL_DIR=$(PROJECT_DIR)/bin \
		DOWNLOAD_OPM_VERSION=$(OPM_VERSION)
	opm version
	@echo "Opm downloaded successfully."

.PHONY: shellcheck
shellcheck: ## Download shellcheck locally if necessary and run against bash  scripts
	@echo "Downloading shellcheck..."
	$(MAKE) -C $(PROJECT_DIR)/telco5g-konflux/scripts/download download-shellcheck DOWNLOAD_INSTALL_DIR=$(PROJECT_DIR)/bin
	@echo "Shellcheck downloaded successfully."
	shellcheck -V
	@echo "Running shellcheck on repository bash files..."
	find . -name '*.sh' \
		-not -path './vendor/*' \
		-not -path './*/vendor/*' \
		-not -path './git/*' \
		-not -path './bin/*' \
		-not -path './testbin/*' \
		-not -path './telco5g-konflux/*' \
		-print0 \
		| xargs -0 --no-run-if-empty shellcheck -x
	@echo "Shellcheck linting completed successfully."

.PHONY: yamllint
yamllint: ## Download yamllint locally if necessary and run against yaml files
	@echo "Downloading yamllint..."
	$(MAKE) -C $(PROJECT_DIR)/telco5g-konflux/scripts/download download-yamllint DOWNLOAD_INSTALL_DIR=$(PROJECT_DIR)/bin
	@echo "Yamllint downloaded successfully."
	yamllint -v
	@echo "Running yamllint on repository YAML files..."
	find . -name "*.yaml" -o -name "*.yml" \
		-not -path './vendor/*' \
		-not -path './*/vendor/*' \
		-not -path './git/*' \
		-not -path './bin/*' \
		-not -path './testbin/*' \
		-not -path './telco5g-konflux/*' \
		-print0 \
		| xargs -0 --no-run-if-empty yamllint -c .yamllint.yaml
	@echo "Yamllint linting completed successfully."

.PHONY: yq
yq: ## Download yq locally if necessary
	@echo "Downloading yq..."
	$(MAKE) -C $(PROJECT_DIR)/telco5g-konflux/scripts/download download-yq DOWNLOAD_INSTALL_DIR=$(PROJECT_DIR)/bin
	yq --version
	@echo "Yq downloaded successfully."

.PHONY: yq-sort-and-format
yq-sort-and-format: yq ## Sort keys/reformat all yaml files 
	@echo "Sorting keys and reformatting YAML files..."
	@find . -name "*.yaml" -o -name "*.yml" | grep -v -E "(telco5g-konflux/|target/|vendor/|bin/|\.git/)" | while read file; do \
		echo "Processing $$file..."; \
		yq -i '.. |= sort_keys(.)' "$$file"; \
	done
	@echo "YAML sorting and formatting completed successfully."

##@ Binary
.PHONY: binary
binary: $(LOCALBIN)
	go build -o $(LOCALBIN)/$(BINARY_NAME) -mod=vendor

.PHONY: crd-watcher
crd-watcher: $(LOCALBIN) ## Build the CRD watcher binary.
	go build -o $(LOCALBIN)/crd-watcher -mod=vendor ./dev-tools/crd-watcher


.PHONY: generate
go-generate:
	go generate ./...
	@for file in *.gen*; do \
		if ! git diff --exit-code -- $$file; then \
			echo "Error: $$file is stale. Please commit the updated file."; \
			exit 1; \
		fi \
	done
	@echo "All generated files are up-to-date."

.PHONY: test tests
test tests:
	@echo "Run ginkgo"
	HWMGR_PLUGIN_NAMESPACE=hwmgr ginkgo run -r ./internal ./api $(ginkgo_flags)

.PHONY: test-e2e
test-e2e: envtest kubectl
ifeq ($(shell uname -s),Linux)
	@chmod -R u+w $(LOCALBIN)
endif
	KUBEBUILDER_ASSETS="$(shell $(ENVTEST) use $(ENVTEST_K8S_VERSION) -i --bin-dir $(LOCALBIN) -p path)" go test ./test/e2e/ -v ginkgo.v

.PHONY: test-crd-watcher
test-crd-watcher:
	@echo "Run crd-watcher unit tests"
	cd dev-tools/crd-watcher && go test -v $(ginkgo_flags)

.PHONY: fmt
fmt:
	@echo "Run fmt"
	gofmt -s -l -w main.go $(SOURCE_DIRS)

.PHONY: vet
vet: ## Run go vet against code.
	go vet ./...

.PHONY: deps-update
deps-update:
	@echo "Update dependencies"
	hack/update_deps.sh
	hack/install_test_deps.sh

# TODO: add back `test-e2e` to ci-job
# NOTE: `bundle-check` should be the last job in the list for `ci-job`
.PHONY: ci-job
ci-job: deps-update go-generate generate fmt vet lint shellcheck bashate fmt test test-crd-watcher bundle-check

.PHONY: clean
clean:
	-rm $(LOCALBIN)/$(BINARY_NAME)

.PHONY: scorecard-test
scorecard-test: operator-sdk
	@test -n "$(KUBECONFIG)" || (echo "The environment variable KUBECONFIG must not empty" && false)
	oc create ns $(OCLOUD_MANAGER_NAMESPACE) --dry-run=client -o yaml | oc apply -f -
	$(OPERATOR_SDK) scorecard bundle -o text --kubeconfig "$(KUBECONFIG)" -n $(OCLOUD_MANAGER_NAMESPACE) --pod-security=restricted

.PHONY: sync-submodules
sync-submodules:
	@echo "Syncing submodules"
	hack/sync-submodules.sh

# markdownlint rules, following: https://github.com/openshift/enhancements/blob/master/Makefile
.PHONY: markdownlint-image
markdownlint-image:  ## Build local container markdownlint-image
	$(CONTAINER_TOOL) image build -f ./hack/Dockerfile.markdownlint --tag $(IMAGE_NAME)-markdownlint:latest ./hack

.PHONY: markdownlint-image-clean
markdownlint-image-clean:  ## Remove locally cached markdownlint-image
	$(CONTAINER_TOOL) image rm $(IMAGE_NAME)-markdownlint:latest

markdownlint: markdownlint-image  ## run the markdown linter
	$(CONTAINER_TOOL) run \
		--rm=true \
		--env RUN_LOCAL=true \
		--env VALIDATE_MARKDOWN=true \
		--env PULL_BASE_SHA=$(PULL_BASE_SHA) \
		-v $$(pwd):/workdir:Z \
		$(IMAGE_NAME)-markdownlint:latest

##@ O-RAN Alarms Server

.PHNOY: alarms
alarms: ##Run full alarms stack
	IMG=$(IMAGE_TAG_BASE):latest make bundle deploy clean-am-service connect-postgres connect-cluster-server run-alarms-migrate create-am-service run-alarms

create-am-service: ##Creates alarm manager service and endpoint to expose a DNS entry.
	oc apply -k ./internal/service/alarms/k8s/base --wait=true
	@echo "Service and Endpoint for alarm manager created."

clean-am-service: ##Deletes alarm manager service and endpoint.
	-oc delete -k ./internal/service/alarms/k8s/base --wait=true --ignore-not-found=true
	@echo "Service and Endpoint for alarm manager deleted."

.PHONY: run-alarms
run-alarms: go-generate binary ##Run alarms server locally
	@oc exec -n $(OCLOUD_MANAGER_NAMESPACE) $(shell oc get pods -n $(OCLOUD_MANAGER_NAMESPACE) -l app=alarms-server -o=jsonpath='{.items[0].metadata.name}') -- cat /var/run/secrets/kubernetes.io/serviceaccount/token > /tmp/token
	TOKEN_PATH=/tmp/token RESOURCE_SERVER_URL="https://localhost:8001" INSECURE_SKIP_VERIFY=true POSTGRES_HOSTNAME=localhost ORAN_O2IMS_ALARMS_PASSWORD=$(ORAN_O2IMS_ALARMS_PASSWORD) $(LOCALBIN)/$(BINARY_NAME) alarms-server serve

run-alarms-migrate: binary ##Migrate all the way up
	DEBUG=yes POSTGRES_HOSTNAME=localhost INSECURE_SKIP_VERIFY=true ORAN_O2IMS_ALARMS_PASSWORD=$(ORAN_O2IMS_ALARMS_PASSWORD) $(LOCALBIN)/$(BINARY_NAME) alarms-server migrate

run-resources-migrate: binary ##Migrate all the way up
	ORAN_O2IMS_RESOURCES_PASSWORD=$(ORAN_O2IMS_RESOURCES_PASSWORD) $(LOCALBIN)/$(BINARY_NAME) resource-server migrate

##@ O-RAN Postgres DB

.PHONY: connect-postgres
connect-postgres: ##Connect to O-RAN postgres
	oc wait --for=condition=Ready pod -l app=postgres-server -n $(OCLOUD_MANAGER_NAMESPACE) --timeout=30s
	@echo "Starting port-forward in background on port 5432:5432 to postgres-server in namespace $(OCLOUD_MANAGER_NAMESPACE)"
	nohup oc port-forward --address localhost svc/postgres-server 5432:5432 -n $(OCLOUD_MANAGER_NAMESPACE) > pgproxy.log 2>&1 &

.PHONY: connect-cluster-server
connect-cluster-server: ##Connect to resource server svc
	@echo "Starting port-forward in background on port 8001:8000 to cluster server svc in namespace $(OCLOUD_MANAGER_NAMESPACE)"
	nohup oc port-forward --address localhost svc/cluster-server 8001:8000 -n $(OCLOUD_MANAGER_NAMESPACE) > pgproxy_resource.log 2>&1 &

##@ Konflux

# You can use podman or docker as a container engine. Notice that there are some options that might be only valid for one of them.
ENGINE ?= docker

.PHONY: konflux-validate-catalog-template-bundle ## validate the last bundle entry on the catalog template file
konflux-validate-catalog-template-bundle: yq operator-sdk
	$(MAKE) -C $(PROJECT_DIR)/telco5g-konflux/scripts/catalog konflux-validate-catalog-template-bundle \
		CATALOG_TEMPLATE_KONFLUX_INPUT=$(PROJECT_DIR)/$(CATALOG_TEMPLATE_KONFLUX_INPUT) \
		CATALOG_TEMPLATE_KONFLUX_OUTPUT=$(PROJECT_DIR)/$(CATALOG_TEMPLATE_KONFLUX_OUTPUT) \
		YQ=$(YQ) \
		OPERATOR_SDK=$(OPERATOR_SDK) \
		ENGINE=$(ENGINE)

.PHONY: konflux-validate-catalog
konflux-validate-catalog: opm ## validate the current catalog file
	$(MAKE) -C $(PROJECT_DIR)/telco5g-konflux/scripts/catalog konflux-validate-catalog \
		CATALOG_KONFLUX=$(PROJECT_DIR)/$(CATALOG_KONFLUX) \
		OPM=$(OPM)

.PHONY: konflux-generate-catalog ## generate a quay.io catalog
konflux-generate-catalog: yq opm
	$(MAKE) -C $(PROJECT_DIR)/telco5g-konflux/scripts/catalog konflux-generate-catalog \
		CATALOG_TEMPLATE_KONFLUX_INPUT=$(PROJECT_DIR)/$(CATALOG_TEMPLATE_KONFLUX_INPUT) \
		CATALOG_TEMPLATE_KONFLUX_OUTPUT=$(PROJECT_DIR)/$(CATALOG_TEMPLATE_KONFLUX_OUTPUT) \
		CATALOG_KONFLUX=$(PROJECT_DIR)/$(CATALOG_KONFLUX) \
		PACKAGE_NAME_KONFLUX=$(PACKAGE_NAME_KONFLUX) \
		BUNDLE_BUILDS_FILE=$(PROJECT_DIR)/.konflux/catalog/bundle.builds.in.yaml \
		OPM=$(OPM) \
		YQ=$(YQ)
	$(MAKE) konflux-validate-catalog

.PHONY: konflux-generate-catalog-production ## generate a registry.redhat.io catalog
konflux-generate-catalog-production: yq opm
	$(MAKE) -C $(PROJECT_DIR)/telco5g-konflux/scripts/catalog konflux-generate-catalog-production \
		CATALOG_TEMPLATE_KONFLUX_INPUT=$(PROJECT_DIR)/$(CATALOG_TEMPLATE_KONFLUX_INPUT) \
		CATALOG_TEMPLATE_KONFLUX_OUTPUT=$(PROJECT_DIR)/$(CATALOG_TEMPLATE_KONFLUX_OUTPUT) \
		CATALOG_KONFLUX=$(PROJECT_DIR)/$(CATALOG_KONFLUX) \
		PACKAGE_NAME_KONFLUX=$(PACKAGE_NAME_KONFLUX) \
		BUNDLE_NAME_SUFFIX=$(BUNDLE_NAME_SUFFIX) \
		PRODUCTION_BUNDLE_NAME=$(PRODUCTION_BUNDLE_NAME) \
		BUNDLE_BUILDS_FILE=$(PROJECT_DIR)/.konflux/catalog/bundle.builds.in.yaml \
		OPM=$(OPM) \
		YQ=$(YQ)
	$(MAKE) konflux-validate-catalog

.PHONY: konflux-filter-unused-redhat-repos
konflux-filter-unused-redhat-repos: ## Filter unused repositories from redhat.repo files in runtime lock folder
	@echo "Filtering unused repositories from runtime lock folder..."
	$(MAKE) -C $(PROJECT_DIR)/telco5g-konflux/scripts/rpm-lock filter-unused-repos REPO_FILE=$(PROJECT_DIR)/.konflux/lock-runtime/redhat.repo
	@echo "Filtering completed for runtime lock folder."

.PHONY: konflux-update-tekton-task-refs
konflux-update-tekton-task-refs: ## Update task references in Tekton pipeline files
	@echo "Updating task references in Tekton pipeline files..."
	$(MAKE) -C $(PROJECT_DIR)/telco5g-konflux/scripts/tekton update-task-refs PIPELINE_FILES="$(shell find $(PROJECT_DIR)/.tekton -name '*.yaml' -not -name 'OWNERS' | tr '\n' ' ')"
	@echo "Task references updated successfully."

.PHONY: konflux-compare-catalog
konflux-compare-catalog: ## Compare generated catalog with upstream FBC image
	@echo "Comparing generated catalog with upstream FBC image..."
	$(MAKE) -C $(PROJECT_DIR)/telco5g-konflux/scripts/catalog konflux-compare-catalog \
		CATALOG_KONFLUX=$(PROJECT_DIR)/$(CATALOG_KONFLUX) \
		PACKAGE_NAME_KONFLUX=$(PACKAGE_NAME_KONFLUX) \
		UPSTREAM_FBC_IMAGE=quay.io/redhat-user-workloads/telco-5g-tenant/$(PACKAGE_NAME_KONFLUX)-fbc-4-20:latest

.PHONY: konflux-all
konflux-catalog-all: konflux-validate-catalog-template-bundle konflux-generate-catalog-production  konflux-compare-catalog ## Run all konflux catalog logic
	@echo "All Konflux targets completed successfully."

