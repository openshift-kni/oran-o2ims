#
# Copyright (c) 2023 Red Hat, Inc.
#
# Licensed under the Apache License, Version 2.0 (the "License"); you may not
# use this file except in compliance with the License. You may obtain a copy of
# the License at
#
#   http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS, WITHOUT
# WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied. See the
# License for the specific language governing permissions and limitations under
# the License.
#

# Additional flags to pass to the `ginkgo` command.
ginkgo_flags:=

.PHONY: binary
binary:
	go build

.PHONY: generate
generate:
	go generate ./...

.PHONY: test tests
test tests:
	ginkgo run -r $(ginkgo_flags)

.PHONY: fmt
fmt:
	gofmt -s -l -w .

.PHONY: lint
lint:
	golangci-lint --version
	golangci-lint run

.PHONY: clean
clean: rm -rf \
	o2ims \
	$(NULL)