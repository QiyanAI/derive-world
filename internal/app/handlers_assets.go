// SPDX-License-Identifier: Apache-2.0
package app

import (
	"encoding/hex"
	"errors"
	"net/http"
	"os"
	"time"

	"github.com/QiyanAI/derive-world/internal/assets"
	"github.com/QiyanAI/derive-world/internal/httpx"
	"github.com/QiyanAI/derive-world/internal/objects"
)

func (a *App) assetServiceUnavailable(w http.ResponseWriter, r *http.Request) bool {
	if a.assets != nil {
		return false
	}
	httpx.WriteError(w, r, httpx.NewError(httpx.CodeInternal, "service unavailable"))
	return true
}

func (a *App) listObjectAssets(w http.ResponseWriter, r *http.Request) {
	if a.assetServiceUnavailable(w, r) {
		return
	}
	object, _, ok := a.loadAuthorized(w, r, objects.PermRead)
	if !ok {
		return
	}
	items, err := a.assets.List(r.Context(), principalFrom(r.Context()).User.ID, object.ID)
	if err != nil {
		a.writeAssetError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, struct {
		Items []assets.Asset `json:"items"`
	}{Items: items})
}

// uploadObjectAsset accepts one raw media body. The query names the asset kind
// and an optional display name; the bytes are validated by content, never by
// the client-provided Content-Type.
func (a *App) uploadObjectAsset(w http.ResponseWriter, r *http.Request) {
	if a.assetServiceUnavailable(w, r) {
		return
	}
	principal := principalFrom(r.Context())
	if !a.checkCSRF(w, r, principal) {
		return
	}
	if !a.allowUserLimit(w, r, principal.User.ID, a.assetUploadLimiter, 30,
		"asset_upload_rate_limited", "too many asset uploads") {
		return
	}
	kind, ok := assets.ParseKind(r.URL.Query().Get("kind"))
	if !ok {
		httpx.WriteError(w, r, httpx.NewError(httpx.CodeInvalidRequest, "kind must be image, video, audio or model"))
		return
	}
	object, _, ok := a.loadAuthorized(w, r, objects.PermContribute)
	if !ok {
		return
	}
	limit, _ := assets.LimitFor(kind)
	if r.ContentLength > limit {
		httpx.WriteError(w, r, httpx.NewError(httpx.CodePayloadTooLarge, "asset exceeds the size limit"))
		return
	}
	// A large video or model legitimately takes longer than the global request
	// read timeout. Extend only this handler's read deadline; the body is still
	// bounded by the per-kind ceiling.
	if controller := http.NewResponseController(w); controller != nil {
		_ = controller.SetReadDeadline(time.Now().Add(3 * time.Minute))
	}
	created, err := a.assets.Upload(r.Context(), principal.User.ID, object.ID, kind,
		r.URL.Query().Get("name"), r.Body,
		httpx.RequestIDFromContext(r.Context()),
		httpx.HashIPBytes(a.config.IPHashPepper, httpx.ClientIP(r, a.config.TrustProxyHeaders)))
	if err != nil {
		a.writeAssetError(w, r, err)
		return
	}
	a.notifyUser(principal.User.ID)
	httpx.WriteJSON(w, http.StatusCreated, created)
}

type generateAssetInput struct {
	Kind     string         `json:"kind"`
	Provider string         `json:"provider"`
	Prompt   string         `json:"prompt"`
	Params   map[string]any `json:"params"`
}

// generateObjectAsset runs the configured bounded media provider. The result
// passes through the same validation, quota, authorization and audit path as
// an uploaded file; the request never carries bytes or code.
func (a *App) generateObjectAsset(w http.ResponseWriter, r *http.Request) {
	if a.assetServiceUnavailable(w, r) {
		return
	}
	principal := principalFrom(r.Context())
	if !a.checkCSRF(w, r, principal) {
		return
	}
	if !a.allowUserLimit(w, r, principal.User.ID, a.assetUploadLimiter, 30,
		"asset_upload_rate_limited", "too many asset uploads") {
		return
	}
	object, _, ok := a.loadAuthorized(w, r, objects.PermContribute)
	if !ok {
		return
	}
	body, err := a.readRequestBody(w, r)
	if err != nil {
		a.writeDomainError(w, r, err)
		return
	}
	if field := detectActorOverride(r, body); field != "" {
		a.rejectActorOverride(w, r, &principal.User.ID, field)
		return
	}
	var input generateAssetInput
	if err := httpx.DecodeJSON(w, r, &input, a.config.MaxRequestBytes); err != nil {
		a.writeDomainError(w, r, err)
		return
	}
	kind, ok := assets.ParseKind(input.Kind)
	if !ok {
		httpx.WriteError(w, r, httpx.NewError(httpx.CodeInvalidRequest, "kind must be image, video, audio or model"))
		return
	}
	created, err := a.assets.Generate(r.Context(), principal.User.ID, object.ID, assets.GenerateRequest{
		Kind: kind, Provider: input.Provider, Prompt: input.Prompt, Params: input.Params,
	}, httpx.RequestIDFromContext(r.Context()),
		httpx.HashIPBytes(a.config.IPHashPepper, httpx.ClientIP(r, a.config.TrustProxyHeaders)))
	if err != nil {
		if errors.Is(err, assets.ErrGenerationUnavailable) {
			httpx.WriteError(w, r, httpx.NewError(httpx.CodeInvalidRequest, "generation provider unavailable"))
			return
		}
		a.writeAssetError(w, r, err)
		return
	}
	a.notifyUser(principal.User.ID)
	httpx.WriteJSON(w, http.StatusCreated, created)
}

func (a *App) removeObjectAsset(w http.ResponseWriter, r *http.Request) {
	if a.assetServiceUnavailable(w, r) {
		return
	}
	principal := principalFrom(r.Context())
	if !a.checkCSRF(w, r, principal) {
		return
	}
	object, _, ok := a.loadAuthorized(w, r, objects.PermManage)
	if !ok {
		return
	}
	assetID, ok := parseUUIDParam(r, "asset_id")
	if !ok {
		httpx.WriteError(w, r, httpx.NewError(httpx.CodeNotFound, "not found"))
		return
	}
	if err := a.assets.Remove(r.Context(), principal.User.ID, object.ID, assetID,
		httpx.RequestIDFromContext(r.Context()),
		httpx.HashIPBytes(a.config.IPHashPepper, httpx.ClientIP(r, a.config.TrustProxyHeaders))); err != nil {
		a.writeAssetError(w, r, err)
		return
	}
	a.notifyUser(principal.User.ID)
	w.WriteHeader(http.StatusNoContent)
}

func (a *App) getObjectAsset(w http.ResponseWriter, r *http.Request) {
	if a.assetServiceUnavailable(w, r) {
		return
	}
	object, _, ok := a.loadAuthorized(w, r, objects.PermRead)
	if !ok {
		return
	}
	assetID, ok := parseUUIDParam(r, "asset_id")
	if !ok {
		httpx.WriteError(w, r, httpx.NewError(httpx.CodeNotFound, "not found"))
		return
	}
	asset, file, err := a.assets.Open(r.Context(), principalFrom(r.Context()).User.ID, object.ID, assetID)
	if err != nil {
		a.writeAssetError(w, r, err)
		return
	}
	defer func() { _ = file.Close() }()
	a.serveAsset(w, r, asset, file, "private, no-store")
}

// publicWorldAsset serves an asset frozen into an active publication. Pending,
// declined, revoked and unpublished assets are indistinguishable from missing.
func (a *App) publicWorldAsset(w http.ResponseWriter, r *http.Request) {
	if a.assetServiceUnavailable(w, r) {
		return
	}
	publicationID, ok := parseUUIDParam(r, "id")
	if !ok {
		httpx.WriteError(w, r, httpx.NewError(httpx.CodeNotFound, "not found"))
		return
	}
	assetID, ok := parseUUIDParam(r, "asset_id")
	if !ok {
		httpx.WriteError(w, r, httpx.NewError(httpx.CodeNotFound, "not found"))
		return
	}
	asset, file, err := a.assets.PublicOpen(r.Context(), publicationID, assetID)
	if err != nil {
		a.writeAssetError(w, r, err)
		return
	}
	defer func() { _ = file.Close() }()
	a.serveAsset(w, r, asset, file, "no-store")
}

func (a *App) serveAsset(w http.ResponseWriter, r *http.Request, asset *assets.Asset,
	file *os.File, cacheControl string) {
	if controller := http.NewResponseController(w); controller != nil {
		// Range streams of a large video may outlive the global write timeout.
		_ = controller.SetWriteDeadline(time.Now().Add(10 * time.Minute))
	}
	etag := `"` + hex.EncodeToString(asset.Checksum) + `"`
	w.Header().Set("Content-Type", asset.MIME)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; sandbox")
	w.Header().Set("Content-Disposition", "inline")
	w.Header().Set("Cross-Origin-Resource-Policy", "same-origin")
	w.Header().Set("Cache-Control", cacheControl)
	w.Header().Set("ETag", etag)
	if match := r.Header.Get("If-None-Match"); match == etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	http.ServeContent(w, r, "", asset.CreatedAt, file)
}

func (a *App) writeAssetError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, assets.ErrNotFound):
		httpx.WriteError(w, r, httpx.NewError(httpx.CodeNotFound, "not found"))
	case errors.Is(err, assets.ErrForbidden):
		httpx.WriteError(w, r, httpx.NewError(httpx.CodeForbidden, "forbidden"))
	case errors.Is(err, assets.ErrInvalidKind):
		httpx.WriteError(w, r, httpx.NewError(httpx.CodeInvalidRequest, "invalid asset kind"))
	case errors.Is(err, assets.ErrTooLarge):
		httpx.WriteError(w, r, httpx.NewError(httpx.CodePayloadTooLarge, "asset exceeds the size limit"))
	case errors.Is(err, assets.ErrUnsupportedType):
		httpx.WriteError(w, r, httpx.NewError(httpx.CodeUnsupportedMediaType, "unsupported asset type"))
	case errors.Is(err, assets.ErrQuota):
		httpx.WriteError(w, r, httpx.NewError(httpx.CodeBudgetExceeded, "asset storage budget exceeded"))
	case errors.Is(err, assets.ErrInUse):
		httpx.WriteError(w, r, httpx.NewError(httpx.CodeConflict, "asset is still used by this world"))
	case errors.Is(err, assets.ErrInvalidContent):
		httpx.WriteError(w, r, httpx.NewError(httpx.CodeInvalidRequest, "invalid asset content"))
	default:
		a.logger.Error("asset_error", "request_id", httpx.RequestIDFromContext(r.Context()),
			"error", err.Error())
		httpx.WriteError(w, r, httpx.NewError(httpx.CodeInternal, "internal server error"))
	}
}
