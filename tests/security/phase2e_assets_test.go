// SPDX-License-Identifier: Apache-2.0

//go:build integration

package security_test

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"testing"

	"github.com/google/uuid"
)

func phase2ePNG(t *testing.T) []byte {
	t.Helper()
	source := image.NewRGBA(image.Rect(0, 0, 16, 12))
	for x := 0; x < 16; x++ {
		for y := 0; y < 12; y++ {
			source.SetRGBA(x, y, color.RGBA{R: uint8(x * 12), G: uint8(y * 18), B: 140, A: 255})
		}
	}
	buffer := &bytes.Buffer{}
	if err := png.Encode(buffer, source); err != nil {
		t.Fatalf("encode png: %v", err)
	}
	return buffer.Bytes()
}

func phase2eMP4() []byte {
	return []byte("\x00\x00\x00\x18ftypmp42\x00\x00\x00\x00mp42isom")
}

func phase2eWAV() []byte {
	content := make([]byte, 44)
	copy(content[0:4], "RIFF")
	content[4] = 36
	copy(content[8:12], "WAVE")
	copy(content[12:16], "fmt ")
	content[16] = 16
	content[20] = 1
	content[22] = 1
	content[24] = 0x44
	content[25] = 0xac
	content[28] = 0x44
	content[29] = 0xac
	content[34] = 16
	copy(content[36:40], "data")
	return content
}

func phase2eWorld(t *testing.T, user *testUser, title string) objectResponse {
	t.Helper()
	return user.createObject(t, "open_world", title, map[string]any{
		"premise": "A world that can hold a real photograph.",
		"kinds": []any{
			map[string]any{"name": "Lamp", "hue": 42, "fields": []any{}},
		},
		"nodes": []any{
			map[string]any{"kind_index": 0, "name": "North lamp", "text": "It glows."},
		},
		"links": []any{},
		"rules": []any{},
	})
}

func phase2eUpload(t *testing.T, user *testUser, objectID, kind, name, contentType string,
	content []byte) (int, []byte) {
	t.Helper()
	status, body, _ := user.call(t, http.MethodPost,
		"/api/v1/objects/"+objectID+"/assets?kind="+kind+"&name="+name,
		callOptions{
			RawBody: content,
			Headers: map[string]string{"Content-Type": contentType},
		})
	return status, body
}

func phase2eAsset(t *testing.T, body []byte) (string, string) {
	t.Helper()
	var asset struct {
		ID   string `json:"id"`
		Kind string `json:"kind"`
	}
	if err := json.Unmarshal(body, &asset); err != nil {
		t.Fatalf("decode asset: %v", err)
	}
	if _, err := uuid.Parse(asset.ID); err != nil {
		t.Fatalf("asset id is not a UUID: %q", asset.ID)
	}
	return asset.ID, asset.Kind
}

func phase2eMediaExperience(assetID, caption string) map[string]any {
	return map[string]any{
		"protocol": 1,
		"root": map[string]any{
			"id": "root", "capability": "core.media",
			"props": map[string]any{"kind": "image", "asset_id": assetID, "caption": caption},
		},
	}
}

func TestPhase2EAssetUploadValidationAndLifecycle(t *testing.T) {
	env := newTestEnv(t)
	owner := env.register(t, "p2eowner")
	other := env.register(t, "p2eother")
	world := phase2eWorld(t, owner, "A world with a photograph")

	// SVG is rejected even when the client claims image/svg+xml.
	status, body := phase2eUpload(t, owner, world.ID, "image", "x.svg", "image/svg+xml",
		[]byte(`<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`))
	if status != http.StatusUnsupportedMediaType {
		t.Fatalf("SVG upload status = %d body = %s", status, body)
	}
	// A JPEG/PNG body declared as audio is rejected by magic sniffing.
	status, body = phase2eUpload(t, owner, world.ID, "audio", "tone.mp3", "audio/mpeg", phase2ePNG(t))
	if status != http.StatusUnsupportedMediaType {
		t.Fatalf("PNG-as-audio status = %d body = %s", status, body)
	}
	// A non-member cannot upload at all and learns nothing.
	status, _ = phase2eUpload(t, other, world.ID, "image", "nope.png", "image/png", phase2ePNG(t))
	if status != http.StatusNotFound {
		t.Fatalf("foreign upload status = %d, want 404", status)
	}

	status, body = phase2eUpload(t, owner, world.ID, "image", "photo.png", "image/png", phase2ePNG(t))
	if status != http.StatusCreated {
		t.Fatalf("image upload status = %d body = %s", status, body)
	}
	imageID, imageKind := phase2eAsset(t, body)
	if imageKind != "image" {
		t.Fatalf("asset kind = %q", imageKind)
	}
	status, body = phase2eUpload(t, owner, world.ID, "video", "film.mp4", "video/mp4", phase2eMP4())
	if status != http.StatusCreated {
		t.Fatalf("video upload status = %d body = %s", status, body)
	}
	videoID, _ := phase2eAsset(t, body)
	status, body = phase2eUpload(t, owner, world.ID, "audio", "tone.wav", "audio/wav", phase2eWAV())
	if status != http.StatusCreated {
		t.Fatalf("audio upload status = %d body = %s", status, body)
	}
	audioID, _ := phase2eAsset(t, body)

	// List is membership-scoped.
	status, body, _ = owner.call(t, http.MethodGet, "/api/v1/objects/"+world.ID+"/assets", callOptions{})
	if status != http.StatusOK || !bytes.Contains(body, []byte(imageID)) {
		t.Fatalf("owner list status = %d body = %s", status, body)
	}
	status, _, _ = other.call(t, http.MethodGet, "/api/v1/objects/"+world.ID+"/assets", callOptions{})
	if status != http.StatusNotFound {
		t.Fatalf("foreign list status = %d, want 404", status)
	}
	status, _, _ = other.call(t, http.MethodGet,
		"/api/v1/objects/"+world.ID+"/assets/"+imageID, callOptions{})
	if status != http.StatusNotFound {
		t.Fatalf("foreign asset read status = %d, want 404", status)
	}

	// Same-origin bytes and byte ranges.
	status, body, headers := owner.call(t, http.MethodGet,
		"/api/v1/objects/"+world.ID+"/assets/"+imageID, callOptions{})
	if status != http.StatusOK || len(body) == 0 || headers.Get("Content-Type") != "image/png" {
		t.Fatalf("asset read status = %d type = %q size = %d", status, headers.Get("Content-Type"), len(body))
	}
	if headers.Get("X-Content-Type-Options") != "nosniff" {
		t.Fatal("asset response is missing nosniff")
	}
	status, body, headers = owner.call(t, http.MethodGet,
		"/api/v1/objects/"+world.ID+"/assets/"+imageID,
		callOptions{Headers: map[string]string{"Range": "bytes=0-7"}})
	if status != http.StatusPartialContent || len(body) != 8 {
		t.Fatalf("range status = %d size = %d", status, len(body))
	}
	if headers.Get("Content-Range") == "" {
		t.Fatal("range response is missing Content-Range")
	}

	// A media reference must exist and match its expected kind.
	missing := uuid.NewString()
	status, _, _ = owner.postAction(t, world.ID, "set_experience",
		map[string]any{"experience": phase2eMediaExperience(missing, "missing")}, world.Version)
	if status != http.StatusBadRequest {
		t.Fatalf("missing asset reference status = %d, want 400", status)
	}
	status, _, _ = owner.postAction(t, world.ID, "set_experience",
		map[string]any{"experience": phase2eMediaExperience(videoID, "wrong kind")}, world.Version)
	if status != http.StatusBadRequest {
		t.Fatalf("wrong-kind asset reference status = %d, want 400", status)
	}
	status, _, _ = owner.postAction(t, world.ID, "set_experience",
		map[string]any{"experience": phase2eMediaExperience(imageID, "the real photo")}, world.Version)
	if status != http.StatusOK {
		t.Fatalf("valid asset experience status = %d", status)
	}
	// The object now contains the media reference.
	status, body, _ = owner.call(t, http.MethodGet, "/api/v1/objects/"+world.ID, callOptions{})
	if status != http.StatusOK || !bytes.Contains(body, []byte(imageID)) {
		t.Fatalf("object read status = %d body = %s", status, body)
	}
	worldNow := decodeObject(t, body)

	// Removing a referenced asset is rejected; clearing the reference first
	// makes the DELETE idempotent.
	status, _, _ = owner.call(t, http.MethodDelete,
		"/api/v1/objects/"+world.ID+"/assets/"+imageID, callOptions{})
	if status != http.StatusConflict {
		t.Fatalf("in-use delete status = %d, want 409", status)
	}
	status, _, _ = owner.postAction(t, world.ID, "set_experience",
		map[string]any{"experience": nil}, worldNow.Version)
	if status != http.StatusOK {
		t.Fatalf("clear experience status = %d", status)
	}
	status, _, _ = owner.call(t, http.MethodDelete,
		"/api/v1/objects/"+world.ID+"/assets/"+imageID, callOptions{})
	if status != http.StatusNoContent {
		t.Fatalf("delete status = %d, want 204", status)
	}
	status, _, _ = owner.call(t, http.MethodGet,
		"/api/v1/objects/"+world.ID+"/assets/"+imageID, callOptions{})
	if status != http.StatusNotFound {
		t.Fatalf("deleted asset read status = %d, want 404", status)
	}

	// The upload and removal are auditable, and no bytes entered object state.
	var uploads, removals int
	if err := env.store.Pool().QueryRow(context.Background(),
		`SELECT count(*) FROM audit_events WHERE action = 'asset.upload'`).Scan(&uploads); err != nil {
		t.Fatalf("count uploads: %v", err)
	}
	if err := env.store.Pool().QueryRow(context.Background(),
		`SELECT count(*) FROM audit_events WHERE action = 'asset.remove'`).Scan(&removals); err != nil {
		t.Fatalf("count removals: %v", err)
	}
	if uploads < 3 || removals < 1 {
		t.Fatalf("audit counts uploads=%d removals=%d", uploads, removals)
	}
	if bytes.Contains(body, []byte("data:image")) {
		t.Fatal("asset bytes leaked into the object response")
	}
	_ = audioID
}

func TestPhase2EAssetDeriveAndPublicationAccess(t *testing.T) {
	env := newTestEnv(t)
	owner := env.register(t, "p2epublish")
	visitor := env.register(t, "p2evisitor")
	world := phase2eWorld(t, owner, "A publishable media world")

	status, body := phase2eUpload(t, owner, world.ID, "image", "photo.png", "image/png", phase2ePNG(t))
	if status != http.StatusCreated {
		t.Fatalf("upload status = %d body = %s", status, body)
	}
	assetID, _ := phase2eAsset(t, body)
	status, body, updated := owner.postAction(t, world.ID, "set_experience",
		map[string]any{"experience": phase2eMediaExperience(assetID, "the photo")}, world.Version)
	if status != http.StatusOK {
		t.Fatalf("set experience status = %d body = %s", status, body)
	}

	// Private derive clones the reference for the same bytes.
	status, body, _ = owner.call(t, http.MethodPost, "/api/v1/objects/"+world.ID+"/derive", callOptions{
		IdempotencyKey: newIdempotencyKey(),
		Body: map[string]any{
			"title": "Child with the same photo", "source_version": updated.Version,
			"inheritance_fields": []string{"premise", "structure", "views"},
		},
	})
	if status != http.StatusCreated {
		t.Fatalf("derive status = %d body = %s", status, body)
	}
	child := decodeObject(t, body)
	status, _, _ = owner.call(t, http.MethodGet,
		"/api/v1/objects/"+child.ID+"/assets/"+assetID, callOptions{})
	if status != http.StatusOK {
		t.Fatalf("child asset status = %d", status)
	}

	// Publication freezes the asset set; anonymous reads work only while active.
	status, body, _ = owner.call(t, http.MethodPost,
		"/api/v1/objects/"+world.ID+"/publications", callOptions{
			IdempotencyKey: newIdempotencyKey(),
			Body:           map[string]any{"derive_allowed": true, "license": "cc_by_4.0"},
		})
	if status != http.StatusCreated {
		t.Fatalf("publication status = %d body = %s", status, body)
	}
	var publication struct {
		ID     string `json:"id"`
		Status string `json:"status"`
	}
	if err := json.Unmarshal(body, &publication); err != nil {
		t.Fatalf("decode publication: %v", err)
	}
	if publication.Status != "active" {
		t.Fatalf("publication status = %s", publication.Status)
	}
	anonymous := newClient(t)
	status, body, headers := env.call(t, anonymous, http.MethodGet,
		"/api/v1/public/worlds/"+publication.ID+"/assets/"+assetID, callOptions{})
	if status != http.StatusOK || headers.Get("Content-Type") != "image/png" {
		t.Fatalf("public asset status = %d type = %q body = %s",
			status, headers.Get("Content-Type"), body)
	}
	// A public derived child keeps a private copy of the reference.
	status, body, _ = visitor.call(t, http.MethodPost,
		"/api/v1/public/worlds/"+publication.ID+"/derive", callOptions{
			IdempotencyKey: newIdempotencyKey(),
			Body: map[string]any{
				"title": "Visitor branch", "inheritance_fields": []string{"premise", "structure", "views"},
			},
		})
	if status != http.StatusCreated {
		t.Fatalf("public derive status = %d body = %s", status, body)
	}
	publicChild := decodeObject(t, body)
	status, _, _ = visitor.call(t, http.MethodGet,
		"/api/v1/objects/"+publicChild.ID+"/assets/"+assetID, callOptions{})
	if status != http.StatusOK {
		t.Fatalf("public-derived child asset status = %d", status)
	}
	// Revocation closes the anonymous route immediately; the private derived
	// child still owns its copied reference.
	status, _, _ = owner.call(t, http.MethodDelete,
		"/api/v1/publications/"+publication.ID, callOptions{})
	if status != http.StatusNoContent {
		t.Fatalf("revoke status = %d", status)
	}
	status, _, _ = env.call(t, anonymous, http.MethodGet,
		"/api/v1/public/worlds/"+publication.ID+"/assets/"+assetID, callOptions{})
	if status != http.StatusNotFound {
		t.Fatalf("revoked public asset status = %d, want 404", status)
	}
	status, _, _ = visitor.call(t, http.MethodGet,
		"/api/v1/objects/"+publicChild.ID+"/assets/"+assetID, callOptions{})
	if status != http.StatusOK {
		t.Fatalf("derived child asset after revoke = %d", status)
	}
}

func TestPhase2EProceduralGenerationUsesTheAssetPath(t *testing.T) {
	env := newTestEnv(t)
	owner := env.register(t, "p2egenerate")
	world := phase2eWorld(t, owner, "A generated backdrop world")
	status, body, _ := owner.call(t, http.MethodPost,
		"/api/v1/objects/"+world.ID+"/asset-generation", callOptions{
			Body: map[string]any{
				"kind": "image", "provider": "procedural",
				"params": map[string]any{"hue": 210, "hue2": 280, "lightness": 12, "stars": 20, "seed": 3},
			},
		})
	if status != http.StatusCreated {
		t.Fatalf("generate status = %d body = %s", status, body)
	}
	assetID, kind := phase2eAsset(t, body)
	if kind != "image" {
		t.Fatalf("generated kind = %q", kind)
	}
	status, content, headers := owner.call(t, http.MethodGet,
		"/api/v1/objects/"+world.ID+"/assets/"+assetID, callOptions{})
	if status != http.StatusOK || headers.Get("Content-Type") != "image/png" || len(content) == 0 {
		t.Fatalf("generated asset status = %d type = %q", status, headers.Get("Content-Type"))
	}
}
