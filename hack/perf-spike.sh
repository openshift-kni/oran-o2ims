#!/bin/bash
#
# SPDX-FileCopyrightText: Red Hat
#
# SPDX-License-Identifier: Apache-2.0
#
# Feasibility spike for the O2ims_InfrastructurePerformance service (see
# docs/proposals/o2ims-performance.md, Phase -1). Validates three risks against a
# live ACM Observability hub + managed cluster, read-only:
#   1. Identity join  : can a Prometheus node series be resolved to an inventory
#                       ClusterResource/Resource by string identity?
#   2. Coverage       : what node/cluster metrics are actually in the allowlist
#                       and in Thanos?
#   3. Hardware        : is any physical-server telemetry present at all?
# Plus an end-to-end query_range proof for one golden measure.
#
# Requires: oc (logged into the hub), curl, jq. Nothing is modified.

set -uo pipefail

NAMESPACE="open-cluster-management-observability"
THANOS_SVC="observability-thanos-query"
THANOS_PORT="9090"
THANOS_URL=""
SPOKE_CONTEXT=""
PF_PID=""

function usage {
    cat <<EOF >&2
Usage: $0 [options]

Options:
    --thanos-url <url>        Use an existing Thanos Query base URL (skip port-forward),
                             e.g. http://localhost:9090
    --namespace <ns>         ACM observability namespace (default: ${NAMESPACE})
    --thanos-svc <name>      Thanos Query service name (default: ${THANOS_SVC})
    --port <port>            Local/remote Thanos port (default: ${THANOS_PORT})
    --spoke-context <ctx>    oc context of a managed cluster, to list its k8s node
                             names for the identity-join comparison (optional)
    -h, --help               Show this help

Example:
    $0 --spoke-context spoke-01
EOF
    exit 1
}

while [[ $# -gt 0 ]]; do
    case "$1" in
        --thanos-url) THANOS_URL="$2"; shift 2 ;;
        --namespace) NAMESPACE="$2"; shift 2 ;;
        --thanos-svc) THANOS_SVC="$2"; shift 2 ;;
        --port) THANOS_PORT="$2"; shift 2 ;;
        --spoke-context) SPOKE_CONTEXT="$2"; shift 2 ;;
        -h|--help) usage ;;
        *) echo "Unknown option: $1" >&2; usage ;;
    esac
done

function hr    { printf '\n========== %s ==========\n' "$1"; }
function note  { printf '  >> %s\n' "$1"; }
function need  { command -v "$1" >/dev/null 2>&1 || { echo "missing required tool: $1" >&2; exit 1; }; }

need oc; need curl; need jq

function cleanup {
    [[ -n "${PF_PID}" ]] && kill "${PF_PID}" >/dev/null 2>&1
}
trap cleanup EXIT

# --- Thanos access ---------------------------------------------------------
if [[ -z "${THANOS_URL}" ]]; then
    hr "Setup: port-forward to ${THANOS_SVC} in ${NAMESPACE}"
    oc -n "${NAMESPACE}" port-forward "svc/${THANOS_SVC}" "${THANOS_PORT}:${THANOS_PORT}" >/dev/null 2>&1 &
    PF_PID=$!
    THANOS_URL="http://localhost:${THANOS_PORT}"
    sleep 4
fi
note "Thanos Query: ${THANOS_URL}"
if ! curl -sf "${THANOS_URL}/api/v1/status/buildinfo" >/dev/null 2>&1; then
    echo "ERROR: cannot reach Thanos Query at ${THANOS_URL}" >&2
    echo "       check the namespace/service names, or pass --thanos-url" >&2
    exit 1
fi
note "reachable."

METRICS_FILE="$(mktemp)"
curl -s "${THANOS_URL}/api/v1/label/__name__/values" | jq -r '.data[]' 2>/dev/null | sort > "${METRICS_FILE}"

# --- Risk #2: coverage -----------------------------------------------------
hr "Risk #2: allowlist & Thanos coverage"
note "default allowlist configmap:"
oc -n "${NAMESPACE}" get cm observability-metrics-allowlist >/dev/null 2>&1 \
    && echo "    present (oc -n ${NAMESPACE} get cm observability-metrics-allowlist -o yaml)" \
    || echo "    NOT FOUND"
note "custom allowlist configmap:"
oc -n "${NAMESPACE}" get cm observability-metrics-custom-allowlist >/dev/null 2>&1 \
    && echo "    present (custom metrics are enabled)" \
    || echo "    none (adding metrics would require creating it = touching ACM config)"

note "total distinct metric names in Thanos: $(wc -l < "${METRICS_FILE}")"
note "node/cluster-scoped metrics present:"
grep -E '^(node_|:node_|instance:node_|node:|cluster:|kube_node_)' "${METRICS_FILE}" | sed 's/^/    /' || echo "    (none)"

note "golden-set presence check:"
for m in node_cpu_seconds_total node_memory_MemAvailable_bytes node_memory_MemTotal_bytes \
         node_filesystem_avail_bytes node_filesystem_size_bytes \
         node_network_receive_bytes_total node_network_transmit_bytes_total node_disk_io_time_seconds_total; do
    if grep -qx "$m" "${METRICS_FILE}"; then echo "    [ok ] $m"; else echo "    [MISS] $m"; fi
done
note "metadata sample (TYPE/HELP drive auto-generation):"
curl -s "${THANOS_URL}/api/v1/metadata" | jq '{node_cpu_seconds_total, node_memory_MemAvailable_bytes}' 2>/dev/null

# --- Risk #1: identity join ------------------------------------------------
hr "Risk #1: identity join (Prometheus series -> inventory entity)"
note "label set on a node series (note cluster / node / instance values):"
curl -s "${THANOS_URL}/api/v1/series?match[]=node_memory_MemAvailable_bytes" \
    | jq '.data[0] // "NO SERIES - metric not collected"' 2>/dev/null

note "does node-exporter carry a 'node' label, or only 'instance'?"
curl -s "${THANOS_URL}/api/v1/series?match[]=node_memory_MemAvailable_bytes" \
    | jq -r '[.data[0] // {} | keys[]] | join(", ")' 2>/dev/null | sed 's/^/    labels: /'
note "kube_node_info present (needed to normalize instance->node)?"
curl -s "${THANOS_URL}/api/v1/series?match[]=kube_node_info" \
    | jq '.data[0] // "ABSENT"' 2>/dev/null

note "inventory side - Agents (become ClusterResource.Name / .NodeClusterName):"
oc get agent -A -o custom-columns=\
HOST:.spec.hostname,CLUSTER:.spec.clusterDeploymentName.name,ROLE:.status.role 2>/dev/null \
    | sed 's/^/    /' || echo "    (no Agent CRs found)"
note "managed clusters (compare to Prometheus 'cluster' label):"
oc get managedcluster -o custom-columns=NAME:.metadata.name 2>/dev/null | sed 's/^/    /' \
    || echo "    (cannot list managedclusters)"
if [[ -n "${SPOKE_CONTEXT}" ]]; then
    note "k8s node names on ${SPOKE_CONTEXT} (compare to Prometheus 'node' label):"
    oc --context "${SPOKE_CONTEXT}" get nodes -o custom-columns=NAME:.metadata.name 2>/dev/null \
        | sed 's/^/    /' || echo "    (cannot reach spoke context ${SPOKE_CONTEXT})"
fi
echo "  ?? COMPARE: Prometheus cluster == Agent clusterDeploymentName == ManagedCluster name?"
echo "  ?? COMPARE: Prometheus node   == Agent hostname == k8s node name?"

# --- Risk #3: hardware telemetry -------------------------------------------
hr "Risk #3: physical-server (hardware) telemetry"
HW=$(grep -Ei 'hwmon|_power_|ipmi_|redfish_|_fan_|_temp_' "${METRICS_FILE}")
if [[ -n "${HW}" ]]; then echo "${HW}" | sed 's/^/    /'; else note "none - physical-Resource layer would be empty-for-now"; fi

# --- End-to-end proof ------------------------------------------------------
hr "End-to-end: one golden recipe via query_range (cpuAverageUtilization)"
END=$(date +%s); START=$((END-3600))
curl -s "${THANOS_URL}/api/v1/query_range" \
    --data-urlencode 'query=100*(1-avg by (cluster,instance)(rate(node_cpu_seconds_total{mode="idle"}[5m])))' \
    --data-urlencode "start=${START}" --data-urlencode "end=${END}" --data-urlencode "step=300" \
    | jq '.data.result[0] // "NO RESULT - check node_cpu_seconds_total is collected and has an instance label"' 2>/dev/null

hr "Done"
echo "Record findings in docs/spike/perf-spike-findings.md (go/no-go per risk + exact join key)."
rm -f "${METRICS_FILE}"

