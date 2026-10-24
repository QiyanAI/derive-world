// SPDX-License-Identifier: Apache-2.0

// Package openapi embeds the canonical OpenAPI 3.1 document.
package openapi

import _ "embed"

// Spec is the OpenAPI 3.1 contract served at /api/v1/openapi.yaml.
//
//go:embed openapi.yaml
var Spec []byte
