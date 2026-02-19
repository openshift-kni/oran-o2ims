# {SchemaName} deprecated fields migration guide

This guide covers the migration from deprecated fields in the `{SchemaName}` schema.

## Overview

| Releases | Field          | Replacement    | Reason                          |
|----------|----------------|----------------|---------------------------------|
| all      | `{fieldName}`  | `{newField}`   | {Brief reason for deprecation}  |

---

## Timeline

| Date           | Action                                                                 |
|----------------|------------------------------------------------------------------------|
| {Month Year}   | `{fieldName}` marked as deprecated ({release version}), `{newField}` available |
| {Month Year}   | **Sunset**: `{fieldName}` field removed                                |

---

## Why Deprecated?

{Explain the technical reasons for deprecation. Common reasons include:}

1. **{Reason 1}**: {Explanation}
2. **{Reason 2}**: {Explanation}

{Describe what the replacement provides or why no replacement is needed.}

---

## Migration

### Before (deprecated)

```bash
GET /o2ims-infrastructureInventory/v1/{endpoint}/{id}

# Response:
{
  ...
  "{fieldName}": {deprecatedValue}
}
```

### After

```bash
GET /o2ims-infrastructureInventory/v1/{endpoint}/{id}

# Response:
{
  ...
  "{newField}": {newValue}
}
```

{If applicable, show how to get additional data:}

```bash
# To get {additional data}:
GET /o2ims-infrastructureInventory/v1/{relatedEndpoint}/{id}
```

---

## HTTP Headers

When accessing `/{endpoint}`, you will see deprecation headers:

```http
HTTP/1.1 200 OK
Deprecation: true
Sunset: {Day}, {DD} {Mon} {YYYY} 00:00:00 GMT
Link: <https://github.com/openshift-kni/oran-o2ims/blob/main/docs/deprecations/{schema}-fields.md>; rel="deprecation"
```

---

## Questions?

If you have questions about this migration, please open an issue in the [oran-o2ims repository](https://github.com/openshift-kni/oran-o2ims/issues)
