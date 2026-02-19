# API deprecation

This directory contains migration guides for deprecated API fields and features.

## Current Status

As of February 2026 (O2IMS v11 compliance update), all previously deprecated fields have been **removed**:

| Schema | Removed Field | Replacement |
|--------|---------------|-------------|
| ResourceType | `alarmDictionary` | `alarmDictionaryId` field + `/alarmDictionaries` endpoint |
| ResourcePool | `oCloudId` | None (O-Cloud context is implicit in API path) |
| ResourcePool | `globalLocationId` | `oCloudSiteId` + `/oCloudSites` and `/locations` endpoints |
| ResourcePool | `location` | `/locations` endpoint with rich LocationInfo model |

There are currently **no deprecated fields** in the API. When new deprecations are introduced, migration guides will be added to this directory.

## Deprecated fields

When fields are deprecated, each schema type will have a dedicated markdown file in this directory. Refer to those guides for details on deprecated fields, timelines, and migration steps.

## Deprecation Policy

Our API follows [RFC 8594](https://datatracker.ietf.org/doc/html/rfc8594) for deprecation signaling:

1. **Deprecation Header**: `Deprecation: true` - Indicates the response contains deprecated fields
2. **Sunset Header**: `Sunset: <date>` - The date when deprecated fields will be removed
3. **Link Header**: `Link: <url>; rel="deprecation"` - Points to migration documentation

## Timeline

- **Deprecation date**: the date when the field is officially marked as deprecated (signaled in the OpenAPI spec and included via HTTP headers). After this date, the field remains available but is marked for removal; users should begin migrating away from it.
- **Sunset date**: the date when the field will be fully removed from API responses (the "hard sunset"). After this date, the field will no longer be present in the API, and any usage should have been migrated to alternatives.

## Implementation

API deprecation signaling is handled via middleware that injects `Deprecation`, `Sunset`, and `Link` headers into HTTP responses.

### Implementation guide: how to deprecate an API field

Follow the instructions below whenever you need to mark a field as deprecated.

You must update **three files**:

1. **OpenAPI spec** (`openapi.yaml`) - Add `deprecated: true` to the field
2. **Migration guide** (`docs/deprecations/<schema>-fields.md`) - Create or update the migration guide:
   - Use the [TEMPLATE.md](TEMPLATE.md) as a starting point for new guides
   - Add the field to the Overview table with versions affected
   - Document why the field is deprecated
   - Provide migration steps with before/after examples
3. **Deprecation registry** ([`internal/service/common/deprecation/registry.go`](../../internal/service/common/deprecation/registry.go)) - Add an entry with:
   - Endpoint path, schema name, and field name
   - Sunset date (when the field will be removed)
   - Replacement field (if any)
   - Link to the migration guide

The registry is the **single source of truth** for deprecation metadata and the middleware reads from it to set HTTP headers. If you mark a field as deprecated in OpenAPI but forget to add it to the registry, **no deprecation headers will be sent**.

## Questions?

If you have questions about migrating from deprecated fields, please open an issue in the [oran-o2ims repository](https://github.com/openshift-kni/oran-o2ims/issues)
