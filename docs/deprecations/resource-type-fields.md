# ResourceType deprecated fields migration guide

This guide covers the migration from the deprecated `alarmDictionary` field to `alarmDictionaryId`.

## Overview

| Releases | Field             | Replacement         | Reason                                              |
|----------|-------------------|---------------------|-----------------------------------------------------|
| all      | `alarmDictionary` | `alarmDictionaryId` | Large, redundant object replaced by reference (UUID)|

---

## Timeline

| Date | Action |
|------|--------|
| February 2026 | `alarmDictionary` marked as deprecated (O2IMS v11.00 release), `alarmDictionaryId` field available |
| December 2026 | **Sunset**: `alarmDictionary` field removed |

---

## Why Deprecated?

The `alarmDictionary` field embedded the entire `AlarmDictionary` object within the `ResourceType` response. This approach had several drawbacks:

1. **Large payloads**: Alarm dictionaries can be large, bloating every ResourceType response
2. **Redundant data**: The same dictionary was repeated in every ResourceType that referenced it

The new `alarmDictionaryId` field provides a reference (UUID) to the dictionary, which can be fetched separately when needed.

---

## Migration

### Before (deprecated)

```bash
GET /o2ims-infrastructureInventory/v1/resourceTypes/dac5c740-3470-4a6f-ac89-a2770199f682

# Response:
{
  "resourceTypeId": "dac5c740-3470-4a6f-ac89-a2770199f682",
  "name": "compute-node",
  "alarmDictionary": {
    "alarmDictionaryId": "b2f8c7a1-3d4e-5f6a-7b8c-9d0e1f2a3b4c",
    "alarmDictionaryVersion": "1.0.0",
    "entityType": "compute",
    "vendor": "Red Hat",
    "alarmDefinition": [
      {
        "alarmDefinitionId": "abc123",
        "alarmName": "CPU_HIGH",
        "alarmDescription": "CPU utilization exceeded threshold",
        "perceivedSeverity": "WARNING"
      }
    ]
  }
}
```

### After

```bash
GET /o2ims-infrastructureInventory/v1/resourceTypes/dac5c740-3470-4a6f-ac89-a2770199f682

# Response:
{
  "resourceTypeId": "dac5c740-3470-4a6f-ac89-a2770199f682",
  "name": "compute-node",
  "alarmDictionaryId": "b2f8c7a1-3d4e-5f6a-7b8c-9d0e1f2a3b4c"
}
```

To get the full alarm dictionary:

```bash
GET /o2ims-infrastructureInventory/v1/alarmDictionaries/{alarmDictionaryId}

# Or via the resource type endpoint:
GET /o2ims-infrastructureInventory/v1/resourceTypes/{resourceTypeId}/alarmDictionary
```

## Available Endpoints

| Endpoint | Description |
|----------|-------------|
| `GET /alarmDictionaries` | List all alarm dictionaries |
| `GET /alarmDictionaries/{id}` | Get a specific alarm dictionary |
| `GET /resourceTypes/{id}/alarmDictionary` | Get dictionary for a resource type |

---

## HTTP Headers

When accessing `/resourceTypes`, you will see deprecation headers:

```http
HTTP/1.1 200 OK
Deprecation: true
Sunset: Sun, 01 Dec 2026 00:00:00 GMT
Link: <https://github.com/openshift-kni/oran-o2ims/blob/main/docs/deprecations/resource-type-fields.md>; rel="deprecation"
```

---

## Questions?

If you have questions about this migration, please open an issue in the [oran-o2ims repository](https://github.com/openshift-kni/oran-o2ims/issues)
