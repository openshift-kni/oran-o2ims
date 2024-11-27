# API Directory

This directory contains all API-related files.

## Structure

```shell
api
├── README.md
├── generated
│   └── alarms.generated.go           # Generated Go code (do not edit manually)
├── openapi.yaml                      # OpenAPI specification
└── tools
    ├── generate.go                   # Code generation script
    └── oapi-codegen.yaml             # Config file for `oapi-codegen` 
```

## How to Generate Server Side Code

```shell
go generate ./...
```

## How to Generate Client Side Code

Note: This is simply here for convenience and make sure we don't generate any internal endpoint clients

```yaml
# overlay
overlay: 1.0.0
info:
  title: "Example to indicate how to use the OpenAPI Overlay specification (https://github.com/OAI/Overlay-Specification) and only generate external client-side code"
  version: 1.0.0
actions:
- target: $.paths.*.*[?(@.tags[*] == 'internal')]
  description: Remove internal endpoints (noted by internal tag)
  remove: true
```

```yaml
# oapi-codegen-client-config.yaml
# yaml-language-server: $schema=https://raw.githubusercontent.com/oapi-codegen/oapi-codegen/HEAD/configuration-schema.json
package: client
output: client.generated.go

generate:
  client: true
  models: true

output-options:
  overlay:
    path: overlay.yaml
```
