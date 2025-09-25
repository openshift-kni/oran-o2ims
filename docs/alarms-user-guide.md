# O-RAN O2IMS Alarms API Server - User Guide

## Table of Contents

- [Overview](#overview)
- [Prerequisites](#prerequisites)
- [Authentication & Access](#authentication--access)
- [API Operations](#api-operations)
- [Troubleshooting](#troubleshooting)
- [API Reference](#api-reference)

## Overview

The O-RAN O2IMS Alarms API Server provides standardized access to infrastructure alarms from OpenShift clusters managed by ACM. It receives alerts from ACM's Alertmanager and exposes O-RAN O2IMS compliant REST APIs for alarm management and real-time subscriptions.

**API Reference:** Complete documentation is in [`internal/service/alarms/api/openapi.yaml`](../internal/service/alarms/api/openapi.yaml)

## Prerequisites

Ensure these components are running before using the alarms API:

### Quick Health Check

```bash
# 1. ACM Observability
oc get MultiClusterObservability -n open-cluster-management-observability
oc get pods -n open-cluster-management-observability | grep alertmanager

# 2. Test alertmanager API access
curl -k -H "Authorization: Bearer $(oc create token prometheus-k8s -n openshift-monitoring)" \
  https://alertmanager-open-cluster-management-observability.apps.YOUR-CLUSTER.com/api/v2/alerts | jq

# 3. O2IMS Services
oc get pods -n oran-o2ims | grep -E "alarms-server|postgres"

# 4. Alertmanager webhook (should show O2IMS webhook config)
oc -n open-cluster-management-observability get secret alertmanager-config \
  --template='{{ index .data "alertmanager.yaml" }}' | base64 -d | grep -A 3 o2ims
```

All commands should return running pods/resources. If any fail, check the [ACM Observability Documentation](https://docs.redhat.com/en/documentation/red_hat_advanced_cluster_management_for_kubernetes/2.14/html-single/observability/index).

> **⚠️ Critical Dependency:** The alarms service is **completely dependent** on a healthy ACM Observability stack. The O2IMS alarms server only configures the alertmanager webhook - it cannot function without ACM's monitoring infrastructure already running and generating alerts.
>
> **For Development/Testing:** If you need to set up ACM Observability from scratch, see the development configuration guide in [DEVELOPING.md](../internal/service/alarms/DEVELOPING.md#setting-up-acm-observability-for-development).

## Authentication & Access

**Quick Setup (Development/Testing):**

```bash
# Apply test service account and generate token
oc apply -f config/testing/client-service-account-rbac.yaml
export MY_TOKEN=$(oc create token -n oran-o2ims test-client --duration=24h)

# Get API endpoint
export API_URI=$(oc get route -n oran-o2ims -o jsonpath='{.items[?(@.spec.path=="/o2ims-infrastructureMonitoring")].spec.host}')
export BASE_URL="https://${API_URI}/o2ims-infrastructureMonitoring/v1"
```

> **Note**: For OAuth2/OIDC-only configurations, service account tokens may not work. Check logs: `oc logs -n oran-o2ims deployment/alarms-server | grep -i oidc`

**Production:** See [OAuth setup instructions in README](../README.md#oauth-expectationsrequirements) for full OAuth2 configuration.

## API Operations

All examples assume you have set `MY_TOKEN` and `BASE_URL` as shown in the authentication section.

**Note:** For complete endpoint documentation, parameter details, and response schemas, refer to the [OpenAPI specification](../internal/service/alarms/api/openapi.yaml).

### Alarm Management

#### List All Alarms

```bash
curl -s -k -H "Authorization: Bearer ${MY_TOKEN}" "${BASE_URL}/alarms" | jq
```

**Example Response:**

```json
[
  {
    "alarmEventRecordId": "e213f273-b6fa-48db-9aa2-a00df5de5747",
    "alarmDefinitionID": "a1b2c3d4-e5f6-7890-abcd-ef1234567890",
    "probableCauseID": "00000000-0000-0000-0000-000000000000",
    "alarmRaisedTime": "2025-09-10T19:45:16.78Z",
    "alarmChangedTime": "2025-09-25T16:25:12.977032Z",
    "alarmAcknowledged": false,
    "resourceTypeID": "9144edb7-549b-5e86-9f6a-d4443f13ccfc",
    "resourceID": "58d0aac5-ab60-4d49-8d44-9c6c50435bf0",
    "perceivedSeverity": 3,
    "extensions": {
      "alertname": "UpdateAvailable",
      "severity": "info",
      "summary": "Your upstream update recommendation service recommends you update your cluster."
    }
  }
]
```

**Key Fields:**

- `alarmEventRecordId`: Unique identifier for this alarm instance
- `alarmRaisedTime`: When the alarm was first triggered (ISO 8601)
- `alarmAcknowledged`: Whether alarm has been acknowledged by operator
- `perceivedSeverity`: Severity level (1=Critical, 2=Major, 3=Warning, 4=Minor)
- `resourceID`: ID of the affected resource (cluster, node, etc.)
- `extensions`: Additional alert metadata from Prometheus/Alertmanager

#### Get Specific Alarm

```bash
curl -s -k -H "Authorization: Bearer ${MY_TOKEN}" \
  "${BASE_URL}/alarms/e213f273-b6fa-48db-9aa2-a00df5de5747" | jq
```

#### Filter Alarms

```bash
# By severity (3=WARNING)
curl -s -k -H "Authorization: Bearer ${MY_TOKEN}" \
  "${BASE_URL}/alarms?filter=(eq,perceivedSeverity,3)" | jq

# By acknowledgment status
curl -s -k -H "Authorization: Bearer ${MY_TOKEN}" \
  "${BASE_URL}/alarms?filter=(eq,alarmAcknowledged,false)" | jq
```

#### Acknowledge an Alarm

```bash
curl -s -k \
  -H "Authorization: Bearer ${MY_TOKEN}" \
  -H "Content-Type: application/merge-patch+json" \
  -X PATCH \
  -d '{"alarmAcknowledged": true}' \
  "${BASE_URL}/alarms/ALARM_ID" | jq
```

### Subscription Management

**⚠️ Prerequisites:** Requires SMO configuration. Without SMO: _"provisioning of Alarm Subscriptions is blocked until the SMO attributes are configured"_. See [SMO registration guide in README](../README.md#registering-the-o-cloud-manager-with-the-smo) for setup instructions.

#### Create Subscription

```bash
CONSUMER_UUID=$(uuidgen)
curl -s -k \
  -H "Authorization: Bearer ${MY_TOKEN}" \
  -H "Content-Type: application/json" \
  -X POST \
  -d "{
    \"consumerSubscriptionId\": \"${CONSUMER_UUID}\",
    \"filter\": \"NEW\",
    \"callback\": \"https://your-webhook-endpoint.com/alarms\"
  }" \
  "${BASE_URL}/alarmSubscriptions" | jq
```

**Filter Options:** `NEW`, `CHANGE`, `CLEAR`, `ACKNOWLEDGE`

#### Manage Subscriptions

```bash
# List subscriptions
curl -s -k -H "Authorization: Bearer ${MY_TOKEN}" "${BASE_URL}/alarmSubscriptions" | jq

# Delete subscription
curl -s -k -H "Authorization: Bearer ${MY_TOKEN}" -X DELETE \
  "${BASE_URL}/alarmSubscriptions/SUBSCRIPTION_ID"
```

**Webhook Requirements:** Your endpoint must respond to GET requests with 204 status. See [Development Guide](../internal/service/alarms/DEVELOPING.md) for test server example.

## Troubleshooting

### Quick Diagnostics

```bash
# Check service health
oc get pods -n oran-o2ims | grep -E "alarms-server|postgres"
oc logs -n oran-o2ims deployment/alarms-server --tail=20

# Test API access
curl -s -k -H "Authorization: Bearer ${MY_TOKEN}" "${BASE_URL}/alarms" | jq '. | length'
```

### Common Issues

| Problem                 | Quick Fix                                                                                                                                                                                                               |
|-------------------------|-------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| **401/403 Auth errors** | Check token expiration: `oc get sa test-client -n oran-o2ims`                                                                                                                                                           |
| **No alarms returned**  | Check alertmanager: `curl -k -H "Authorization: Bearer $(oc create token prometheus-k8s -n openshift-monitoring)" https://alertmanager-open-cluster-management-observability.apps.YOUR-CLUSTER.com/api/v2/alerts \| jq` |
| **Subscription errors** | Verify SMO configured; check logs: `oc logs -n oran-o2ims deployment/alarms-server \| grep -i subscription`                                                                                                             |
| **500 Database errors** | Check server pod and postgres: `oc get pods -n oran-o2ims \| grep postgres`   `oc exec -it postgres-server-57d7d49f99-2m9c5 -- cat /var/lib/pgsql/data/userdata/pg_log/postgresql.log`                                  |

### Error Messages

| Error                                                                                    | Solution                                              |
|------------------------------------------------------------------------------------------|-------------------------------------------------------|
| `provisioning of Alarm Subscriptions is blocked until the SMO attributes are configured` | Configure SMO settings in Inventory CR                |
| `invalid bearer token`                                                                   | Ensure Keycloak URL matches between client and server |
| `string doesn't match the format "uuid"`                                                 | Use `uuidgen` for consumerSubscriptionId              |

## API Reference

**Complete API documentation is available in the [OpenAPI specification](../internal/service/alarms/api/openapi.yaml)** which includes:

- All endpoint definitions with parameters and responses
- Complete data model schemas (`AlarmEventRecord`, `AlarmSubscriptionInfo`, etc.)
- Authentication requirements and error codes
- Query parameter syntax and filtering operators

---

## Additional Resources

### Key Documentation

- **[OpenAPI Specification](../internal/service/alarms/api/openapi.yaml)** - Complete API reference (endpoints, models, authentication)
- **[Development Guide](../internal/service/alarms/DEVELOPING.md)** - Server development and testing guidance
- **[O-RAN O2IMS Spec](https://specifications.o-ran.org/download?id=674)** - Official specification this server implements

### Dependencies

- **[ACM Observability](https://docs.redhat.com/en/documentation/red_hat_advanced_cluster_management_for_kubernetes/2.14/html-single/observability/index)** - Required for alert ingestion
- **[Prometheus Alertmanager](https://prometheus.io/docs/alerting/latest/alertmanager/)** - Source of alerts processed by this service

---
