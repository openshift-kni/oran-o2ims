/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package codegen

//go:generate go run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen -config configs/provisioning-oapi-codegen-server.yaml ../specs/provisioning.yaml

//go:generate go run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen -config configs/provisioning-oapi-codegen-client.yaml ../specs/provisioning.yaml

//go:generate go run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen -config configs/inventory-oapi-codegen-server.yaml ../specs/inventory.yaml
