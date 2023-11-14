#!/bin/bash
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

# This script starts the servers and the reverse proxy in the local machine. It is intended for
# use in the development environment, where it is convenient to run the servers locally in order
# to debug them. Stop it with Ctr+C, and it will kill the servers.

# Stop the servers when finished:
function clean {
  kill -9 ${pids}
}
trap clean EXIT

# Start the metadata server:
./oran-o2ims start metadata-server \
--log-file="servers.log" \
--log-level="debug" \
--log-field="server=metadata" \
--log-field="pid=%p" \
--api-listener-address="127.0.0.1:8000" \
--cloud-id="123" \
&
pids="${pids} $!"

# Start the deployment manager server:
./oran-o2ims start deployment-manager-server \
--log-file="servers.log" \
--log-level="debug" \
--log-field="server=deployment-manager" \
--log-field="pid=%p" \
--api-listener-address="127.0.0.1:8001" \
--cloud-id="123" \
--backend-url="${BACKEND_URL}" \
--backend-token="${BACKEND_TOKEN}" \
&
pids="${pids} $!"

# Start the resource server:
./oran-o2ims start resource-server \
--log-file="servers.log" \
--log-level="debug" \
--log-field="server=resource" \
--log-field="pid=%p" \
--api-listener-address="127.0.0.1:8002" \
--cloud-id="123" \
--backend-url="${BACKEND_URL}" \
--backend-token="${BACKEND_TOKEN}" \
&
pids="${pids} $!"

# Start the reverse proxy:
podman run \
--rm \
--network="host" \
--volume="${PWD}/proxy.yaml:/etc/proxy.yaml:z" \
--entrypoint="/usr/local/bin/envoy" \
docker.io/envoyproxy/envoy:v1.28.0 \
--config-path "/etc/proxy.yaml"
