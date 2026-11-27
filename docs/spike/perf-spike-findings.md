<!--
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
-->

# O2ims_InfrastructurePerformance — Feasibility Spike Findings

Phase -1 feasibility results (see
[docs/proposals/o2ims-performance.md](../proposals/o2ims-performance.md)),
produced with `hack/perf-spike.sh` against a live ACM Observability hub.

```yaml
run-date: 2026-11-03
hub: lab (cnfdg environment)
managed-clusters: cnfdg60 (SNO), cnfdg67 (SNO), cnfdg70 (SNO), local-cluster (hub)
notes: >
  An initial run was INVALID — observability-thanos-receive-default-0 was
  crash-looping (0/1, 3638 restarts) with "no space left on device" on its PVC, so
  Thanos held only ~8 stale metrics. After the receive PVC was fixed the stack
  returned to ~170-188 metrics and the results below are valid. The latest run had
  NO custom allowlist, yet the core node set was still present (default allowlist).
```

## Verdict summary

| Risk | Result | Go / No-go |
|------|--------|-----------|
| #1 Identity join | `(cluster, instance)` joins cleanly; `instance` = node FQDN = Agent hostname; no correspondence table | ✅ |
| #2 Allowlist / Thanos coverage | Core node metrics present via the default allowlist; network via recording rules; disk I/O + hardware need custom allowlist | ✅ (with gaps) |
| #3 Hardware telemetry | ironic sidecar + ServiceMonitor confirmed; `baremetal_*` sensor set enumerated (thermal/health; thin on watts); join `node_name` → BMH → `Resource`. Remaining: allowlist `baremetal_*` into ACM Thanos | ✅ confirmed; ⚠️ allowlist pending |
| E2E translation proof | `cpuAverageUtilization` returned live values, grouped by `(cluster, instance)` — per-node grouping works | ✅ |

**Overall:** ✅ proceed — viable for the k8s-node + cluster layers; hardware source
exists and is wired (sidecar + ServiceMonitor), needing only a custom-allowlist
entry to reach ACM Thanos. Validate multi-node before relying on per-node
attribution.

## Risk #1 — Identity join (make-or-break) — GREEN

Observed node series (`node_memory_MemAvailable_bytes`):
`{cluster="cnfdg60", clusterID="3d828c9f-…", clusterType="SNO",
instance="cnfdg60.ptp.eng.rdu2.dc.redhat.com", job="node-exporter", …}`

| Field | Prometheus label | Inventory value | Match? |
|-------|------------------|-----------------|--------|
| cluster | `cluster=cnfdg60` | Agent `clusterDeploymentName=cnfdg60` / ManagedCluster `cnfdg60` | ✅ |
| node | `instance=cnfdg60.ptp.eng.rdu2.dc.redhat.com` (**no `node` label**) | Agent `hostname=cnfdg60.ptp…` = `ClusterResource.Name` | ✅ exact |

- **Join key: `(cluster, instance)`**, `instance` = FQDN. No correspondence table needed.
- `kube_node_info` is **ABSENT** — not needed here (instance already = hostname), but
  note the join is config-dependent: it relies on node-exporter `instance` being the FQDN.
- **Recipes/aggregations must group by `instance`, not `node`.**
- Caveat: tested on **SNO only** (node == cluster). Multi-node untested.

## Risk #2 — Allowlist & coverage — GREEN with named gaps

- Total metrics in Thanos: **~170–188** (healthy).
- **Core set is present via the DEFAULT allowlist** — the latest run had **no
  custom allowlist** yet still carried `node_cpu_seconds_total`,
  `node_memory_MemAvailable/MemTotal_bytes`, `node_filesystem_*`, `kube_node_*`,
  and the `instance:node_*:rate1m` recording rules. So **core coverage needs no
  ACM change**.
- **Network:** covered by `instance:node_network_*:rate1m` recording rules; raw
  `node_network_*_bytes_total` not forwarded (by design) — map to the rules.
- **Disk I/O:** `node_disk_io_time_seconds_total` missing, no disk recording rule →
  needs a `observability-metrics-custom-allowlist` addition.

## Risk #3 / #3b — Hardware telemetry — source confirmed, metrics enumerated

- **Hub aggregation confirmed:** `cluster=local-cluster` is present in ACM Thanos.
- **Ironic exporter confirmed as a metal3 sidecar** (`openshift-machine-api/metal3-…`,
  container `metal3-ironic-prometheus-exporter`) with a **ServiceMonitor** → its
  metrics are scraped into the hub's Prometheus (confirmed by querying
  `thanos-querier` with a `cluster-monitoring-view` SA token).

**Useful hardware sensor set** (`baremetal_*`, all GAUGE/status):
`baremetal_temp_cpu_celsius`, `baremetal_temp_systemboard_celsius`,
`baremetal_fan_status`, `baremetal_power_status`, `baremetal_drive_status`,
`baremetal_temperature_status`, `machine_nvm_avg_power_budget_watts`.
Thermal + component-health; **thin on actual power draw** (`power_status` is
state, not watts).

**Excluded:** the ~140 `ironic_*` metrics are Ironic's internal provisioning
operational counters (`..._call_count` / `..._time`) — not server telemetry.

**Identity labels** (from `baremetal_drive_status`):
`node_name="xr8720t-pool~xr8720txdg60"` (`<bmh-namespace>~<bmh-name>`),
`node_uuid`, `redfish_system_uuid`, `managed_cluster` (cluster UUID),
`manufacturer`/`model`/`sensor_id`/`health`/`state`;
`instance` is the exporter endpoint IP:port (not server identity).

**Key cross-layer finding:** the hardware layer and the OS/node layer share **no
common label** — the physical server's BMH name (`xr8720txdg60`) differs from its
OS hostname (`cnfdg60`). So hardware↔node can only be correlated **through the
inventory** (`ClusterResource.ResourceID` hosting edge), not by label matching —
this validates the inventory-bridge design.

- **Hardware join:** `node_name` → `<namespace>~<bmh-name>` → BareMetalHost →
  `bmh.UID` → resource-server `Resource` (or via `redfish_system_uuid`).
- **Still to do:** add the `baremetal_*` subset to
  `observability-metrics-custom-allowlist` and confirm they appear in ACM Thanos
  as `cluster=local-cluster` (single-source).

## End-to-end proof — GREEN

`100*(1-avg by (cluster,instance)(rate(node_cpu_seconds_total{mode="idle"}[5m])))`
returned a series labelled `{cluster="cnfdg60", instance="cnfdg60.ptp…"}` with 10
points (~0.8–77%). The result carrying the `instance` label confirms **per-node
grouping works** (so it extends to multi-node). Counter→rate→percentage
translation works against live Thanos.

## Consequences for the design

- **Single source** via ACM `observability-thanos-query` for all layers
  (hub = `local-cluster`); dual-source hub Prometheus kept only as fallback.
- Join key **`(cluster, instance)`**; group by `instance`; hardware joins on BMH name.
- **Allowlist governs coverage**: network via rules; disk I/O + hardware require a
  custom-allowlist addition (additive ACM change, data-volume cost).
- **Open / to validate next:** multi-node cluster join; ironic exporter presence +
  schema + post-allowlist appearance in Thanos.
