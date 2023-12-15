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

# Details of the image:
image_repo:=quay.io/openshift-kni/oran-o2ims
image_tag:=latest

# Additional flags to pass to the `ginkgo` command.
ginkgo_flags:=

.PHONY: binary
binary:
	go build

.PHONY: image
image:
	podman build -t "$(image_repo):$(image_tag)" -f Containerfile .

.PHONY: push
push: image
	podman push "$(image_repo):$(image_tag)"

.PHONY: generate
generate:
	go generate ./...

.PHONY: test tests
test tests:
	@echo "Run ginkgo"
	ginkgo run -r $(ginkgo_flags)

.PHONY: fmt
fmt:
	@echo "Run fmt"
	gofmt -s -l -w .

.PHONY: lint
lint:
	@echo "Run lint"
	golangci-lint --version
	golangci-lint run --verbose --print-resources-usage --modules-download-mode=vendor --timeout=5m0s

.PHONY: lint-openapi
lint-openapi:
	@echo "Run lint OpenAPI"
	spectral lint --ruleset internal/openapi/lint.yaml internal/openapi/spec.yaml

.PHONY: deps-update
deps-update:
	@echo "Update dependencies"
	hack/update_deps.sh
	hack/install_test_deps.sh

.PHONY: ci-job
ci-job: deps-update lint fmt test

.PHONY: clean
clean:
	rm -rf \
	oran-o2ims \
	$(NULL)
