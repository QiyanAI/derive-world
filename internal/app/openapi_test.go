// SPDX-License-Identifier: Apache-2.0
package app

import (
	"context"
	"strings"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"

	"github.com/QiyanAI/derive-world/api/openapi"
)

func TestOpenAPI(t *testing.T) {
	loader := openapi3.NewLoader()
	document, err := loader.LoadFromData(openapi.Spec)
	if err != nil {
		t.Fatalf("load OpenAPI: %v", err)
	}
	if err := document.Validate(context.Background()); err != nil {
		t.Fatalf("validate OpenAPI: %v", err)
	}
	if !strings.HasPrefix(document.OpenAPI, "3.1") {
		t.Fatalf("OpenAPI version = %q, want 3.1.x", document.OpenAPI)
	}

	publicPaths := map[string]bool{
		"/healthz":              true,
		"/readyz":               true,
		"/api/v1/meta":          true,
		"/api/v1/openapi.yaml":  true,
		"/api/v1/auth/register": true,
		"/api/v1/auth/login":    true,
	}
	bodyRoutes := map[string]bool{
		"POST /api/v1/auth/register":                       true,
		"POST /api/v1/auth/login":                          true,
		"POST /api/v1/objects":                             true,
		"POST /api/v1/objects/{id}/actions":                true,
		"POST /api/v1/objects/{id}/derive":                 true,
		"POST /api/v1/capabilities/{capability_id}/invoke": true,
	}

	specRoutes := map[string]bool{}
	for path, item := range document.Paths.Map() {
		for method, operation := range item.Operations() {
			specRoutes[strings.ToUpper(method)+" "+path] = true
			if operation.OperationID == "" {
				t.Errorf("%s %s has no operationId", method, path)
			}
			security := document.Security
			if operation.Security != nil {
				security = *operation.Security
			}
			if publicPaths[path] {
				if len(security) != 0 {
					t.Errorf("public route %s %s declares security", method, path)
				}
			} else {
				if !hasSecurityScheme(security, "cookieAuth") {
					t.Errorf("protected route %s %s does not require cookieAuth", method, path)
				}
				if operation.Responses.Status(401) == nil {
					t.Errorf("protected route %s %s has no 401 response", method, path)
				}
				if operation.Responses.Status(429) == nil {
					t.Errorf("protected route %s %s has no 429 response", method, path)
				}
			}
			if bodyRoutes[strings.ToUpper(method)+" "+path] {
				if operation.RequestBody == nil {
					t.Errorf("%s %s has no request body schema", method, path)
				}
				if operation.Responses.Status(413) == nil {
					t.Errorf("%s %s has no 413 response", method, path)
				}
				if operation.Responses.Status(415) == nil {
					t.Errorf("%s %s has no 415 response", method, path)
				}
			}
		}
	}

	app := newTestApp(t)
	for _, route := range app.Routes() {
		key := route.Method + " " + route.Pattern
		if !specRoutes[key] {
			t.Errorf("route %s is not in the OpenAPI document", key)
		}
		delete(specRoutes, key)
	}
	for key := range specRoutes {
		t.Errorf("OpenAPI documents route %s that is not registered", key)
	}
}

func hasSecurityScheme(requirements openapi3.SecurityRequirements, name string) bool {
	for _, requirement := range requirements {
		if _, ok := requirement[name]; ok {
			return true
		}
	}
	return false
}
