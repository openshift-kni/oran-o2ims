# ResourcePool deprecated fields migration guide

This guide covers the migration path for deprecated fields in the `ResourcePool` schema.

## Overview

The following fields in `ResourcePool` are deprecated:

| Releases | Field              | Replacement                         | Reason                                             |
|----------|--------------------|-------------------------------------|----------------------------------------------------|
| all      | `oCloudId`         | None (implicit)                     | Redundant, O-Cloud context is implicit in the API  |
| all      | `globalLocationId` | `oCloudSiteId` + `/locations` API   | Replaced by structured LocationInfo model          |
| all      | `location`         | `GET /locations/{globalLocationId}` | Simple string replaced with rich location data     |

---

## Timeline

| Date | Action |
|------|--------|
| February 2026 | Fields marked as deprecated (O2IMS v11.00 release) |
| December 2026 | **Sunset**: fields removed from responses |

---

## Field: `oCloudId`

### Why is `oCloudId` Deprecated?

The `oCloudId` field is redundant because:

- The O-Cloud context is implicit in the API path
- All resources in this API belong to the same O-Cloud instance
- The O-Cloud ID is available via `GET /o2ims-infrastructureInventory/v1` (OCloudInfo)

### Migration Steps for `oCloudId`

**No action required.** Simply stop using this field. The O-Cloud ID can be obtained from:

```bash
# Get O-Cloud information
GET /o2ims-infrastructureInventory/v1

# Response includes oCloudId
{
  "oCloudId": "262c8f17-52b5-4614-9e56-812ae21fa8a7",
  "name": "my-cloud",
  ...
}
```

---

## Field: `globalLocationId`

### Why is `globalLocationId` Deprecated?

This field is being replaced by the new `LocationInfo` model introduced in O-RAN.WG6.TS.O2IMS-INTERFACE-R005-v11.00. The new model provides:

- Structured geographic coordinates (GeoJSON Point)
- Civic addresses following IETF RFC 4776
- Human-readable address strings

### Migration Steps for `globalLocationId`

**Before (deprecated):**

```bash
GET /o2ims-infrastructureInventory/v1/resourcePools/{resourcePoolId}

# Response:
{
  "resourcePoolId": "d3b27e14-4589-4d93-ae76-ddca559193ea",
  "globalLocationId": "262c8f17-52b5-4614-9e56-812ae21fa8a7",
  "name": "my-cluster"
}
```

**After:**

```bash
# Step 1: Get the ResourcePool with oCloudSiteId
GET /o2ims-infrastructureInventory/v1/resourcePools/{resourcePoolId}

# Response:
{
  "resourcePoolId": "d3b27e14-4589-4d93-ae76-ddca559193ea",
  "oCloudSiteId": "7fd364a0-6d82-4e0c-a418-5c1a2f8b9c3e",
  "name": "my-cluster"
}

# Step 2: Get the O-Cloud Site to find the globalLocationId
GET /o2ims-infrastructureInventory/v1/oCloudSites/{oCloudSiteId}

# Response:
{
  "oCloudSiteId": "7fd364a0-6d82-4e0c-a418-5c1a2f8b9c3e",
  "globalLocationId": "location-east-1",
  "name": "East Data Center"
}

# Step 3: Get detailed location information
GET /o2ims-infrastructureInventory/v1/locations/{globalLocationId}

# Response:
{
  "globalLocationId": "location-east-1",
  "name": "East Data Center",
  "coordinate": {
    "type": "Point",
    "coordinates": [-77.0364, 38.8951]
  },
  "civicAddress": [
    {"caType": 1, "caValue": "US"},
    {"caType": 3, "caValue": "Virginia"},
    {"caType": 6, "caValue": "Ashburn"}
  ],
  "address": "123 Data Center Way, Ashburn, VA"
}
```

---

## Field: `location`

### Why is `location` Deprecated?

The `location` field was a simple string (e.g., "EU", "US-EAST") that provided minimal information. The new `/locations` endpoint provides rich, structured location data including:

- **Geographic coordinates** (latitude/longitude) per IETF RFC 7946 (GeoJSON)
- **Civic addresses** per IETF RFC 4776 (country, state, city, street, building)
- **Human-readable addresses** for display purposes

### Migration Steps for `location`

**Before (deprecated):**

```json
{
  "resourcePoolId": "d3b27e14-4589-4d93-ae76-ddca559193ea",
  "location": "EU"
}
```

**After:**

```bash
# Use the new locations API for detailed information
GET /o2ims-infrastructureInventory/v1/locations

# Response:
[
  {
    "globalLocationId": "location-eu-west",
    "name": "EU West Data Center",
    "coordinate": {
      "type": "Point",
      "coordinates": [-0.1276, 51.5074]
    },
    "civicAddress": [
      {"caType": 1, "caValue": "GB"},
      {"caType": 6, "caValue": "London"}
    ],
    "address": "London, United Kingdom"
  }
]
```

## HTTP Headers

When accessing endpoints that return `ResourcePool`, you will see these headers:

```http
HTTP/1.1 200 OK
Deprecation: true
Sunset: Sun, 01 Dec 2026 00:00:00 GMT
Link: <https://github.com/openshift-kni/oran-o2ims/blob/main/docs/deprecations/resource-pool-fields.md>; rel="deprecation"
```

---

## Questions?

If you have questions about this migration, please open an issue in the [oran-o2ims repository](https://github.com/openshift-kni/oran-o2ims/issues)
