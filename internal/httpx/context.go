// SPDX-License-Identifier: Apache-2.0
package httpx

import (
	"context"
	"net/http"
)

type contextKey int

const (
	requestIDKey contextKey = iota
	routeKey
	actorKey
)

// WithRequestID stores a request ID in the context.
func WithRequestID(ctx context.Context, requestID string) context.Context {
	return context.WithValue(ctx, requestIDKey, requestID)
}

// RequestIDFromContext returns the request ID or an empty string.
func RequestIDFromContext(ctx context.Context) string {
	value, _ := ctx.Value(requestIDKey).(string)
	return value
}

// WithRoute stores the matched route template in the context.
func WithRoute(ctx context.Context, route string) context.Context {
	return context.WithValue(ctx, routeKey, route)
}

// RouteFromContext returns the matched route template or an empty string.
func RouteFromContext(ctx context.Context) string {
	value, _ := ctx.Value(routeKey).(string)
	return value
}

// Actor describes an authenticated principal for access logging.
type Actor struct {
	UserID string
	Type   string
}

// WithActor stores the authenticated actor in the context.
func WithActor(ctx context.Context, actor Actor) context.Context {
	return context.WithValue(ctx, actorKey, actor)
}

// ActorFromContext returns the actor, if any.
func ActorFromContext(ctx context.Context) Actor {
	value, _ := ctx.Value(actorKey).(Actor)
	return value
}

// SetActor records the authenticated actor on the response writer when the
// access-log middleware is present, and on the request context for downstream
// handlers.
func SetActor(w http.ResponseWriter, actor Actor) {
	if recorder, ok := w.(interface{ SetActor(Actor) }); ok {
		recorder.SetActor(actor)
	}
}
