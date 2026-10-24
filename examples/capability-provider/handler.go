// SPDX-License-Identifier: Apache-2.0
package main

import (
	"bytes"
	"container/list"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log"
	"mime"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	maxSkewSeconds = 30
	replayWindow   = 300 * time.Second
	maxOutputBytes = 262144
	maxTextRunes   = 48000
	// responseSignatureVersion is the G06 v2 response profile negotiated by
	// the Broker through X-DW-Response-Signature-Version.
	responseSignatureVersion = "v2"
	// Exact D06 envelope overhead for content.structure (fixed keys/values
	// plus one UUIDv7). MAX_BODY_BYTES bounds the business input; the HTTP
	// body cap is MAX_BODY_BYTES + this exact overhead.
	maxEnvelopeOverhead = 126
)

var (
	uuidV7Pattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
	capabilityID  = "content.structure"
)

type invokeRequest struct {
	InvocationID    string          `json:"invocation_id"`
	CapabilityID    string          `json:"capability_id"`
	ContractVersion string          `json:"contract_version"`
	Input           json.RawMessage `json:"input"`
}

type structureInput struct {
	ObjectID string `json:"object_id"`
	Text     string `json:"text"`
	Format   string `json:"format"`
}

type providerError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type successResponse struct {
	InvocationID    string          `json:"invocation_id"`
	CapabilityID    string          `json:"capability_id"`
	ContractVersion string          `json:"contract_version"`
	Status          string          `json:"status"`
	Output          structureOutput `json:"output"`
	DurationMS      int             `json:"duration_ms"`
}

type errorResponse struct {
	InvocationID string        `json:"invocation_id"`
	Error        providerError `json:"error"`
}

// providerKey is one accepted HMAC key. validUntil is enforced per request so
// a rotation deadline invalidates the previous key without a restart.
type providerKey struct {
	id         string
	secret     []byte
	validUntil time.Time
}

func (k providerKey) active(now time.Time) bool {
	return k.validUntil.IsZero() || now.Before(k.validUntil)
}

// responseAuth carries the verified request key and timestamp needed to sign
// the response. It is nil for requests rejected before signature verification.
type responseAuth struct {
	key       providerKey
	timestamp string
	enabled   bool
}

type handler struct {
	keys                   []providerKey
	currentKeyID           string
	requireResponseSigning bool
	providerID             string
	contract               string
	maxBodyBytes           int64
	replay                 *replayCache
	now                    func() time.Time
	executions             atomic.Int32
}

func newHandler(cfg config) http.Handler {
	keyID := cfg.keyID
	if keyID == "" {
		keyID = "k1"
	}
	keys := []providerKey{{id: keyID, secret: append([]byte(nil), cfg.secret...)}}
	if len(cfg.previousSecret) > 0 {
		keys = append(keys, providerKey{
			id: cfg.previousKeyID, secret: append([]byte(nil), cfg.previousSecret...),
			validUntil: cfg.previousUntil,
		})
	}
	return &handler{
		keys: keys, currentKeyID: keyID,
		requireResponseSigning: cfg.requireResponseSigning,
		providerID:             cfg.providerID, contract: cfg.contract,
		maxBodyBytes: cfg.maxBodyBytes, replay: newReplayCache(cfg.replaySize),
		now: time.Now,
	}
}

func (h *handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/healthz":
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"status": "method_not_allowed"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	case "/v1/invoke":
		h.invoke(w, r)
	default:
		writeJSON(w, http.StatusNotFound, map[string]string{"status": "not_found"})
	}
}

func (h *handler) invoke(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		h.writeError(w, nil, "", http.StatusMethodNotAllowed, "invalid_request", "method not allowed")
		return
	}
	contentType := r.Header.Get("Content-Type")
	if mediaType, _, err := mime.ParseMediaType(contentType); err != nil ||
		!strings.EqualFold(mediaType, "application/json") {
		h.writeError(w, nil, "", http.StatusBadRequest, "invalid_request", "content type must be application/json")
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, h.maxBodyBytes+maxEnvelopeOverhead))
	if err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			h.writeError(w, nil, "", http.StatusRequestEntityTooLarge, "payload_too_large", "request body exceeds the limit")
			return
		}
		h.writeError(w, nil, "", http.StatusBadRequest, "invalid_request", "request body could not be read")
		return
	}
	matchedKey, err := h.verifySignature(r, body)
	if err != nil {
		// Signature failures are intentionally uniform: no reason is exposed
		// and no signature or secret is logged. There is no trusted key to
		// sign this response with.
		h.writeError(w, nil, "", http.StatusUnauthorized, "invalid_request", "invalid request signature")
		return
	}
	auth := &responseAuth{key: matchedKey, timestamp: r.Header.Get("X-DW-Timestamp")}
	var request invokeRequest
	if err := decodeStrict(body, &request); err != nil {
		h.writeError(w, auth, looseInvocationID(body), http.StatusBadRequest,
			"invalid_request", "invalid invocation envelope")
		return
	}
	v2Requested := r.Header.Get("X-DW-Response-Signature-Version") == responseSignatureVersion
	auth.enabled = v2Requested || h.requireResponseSigning
	if h.requireResponseSigning && !v2Requested {
		h.writeError(w, auth, request.InvocationID, http.StatusBadRequest,
			"invalid_request", "a signed response is required")
		return
	}
	if int64(len(request.Input)) > h.maxBodyBytes {
		h.writeError(w, auth, request.InvocationID, http.StatusRequestEntityTooLarge,
			"payload_too_large", "input exceeds the size limit")
		return
	}
	if !uuidV7Pattern.MatchString(request.InvocationID) ||
		request.CapabilityID != capabilityID ||
		request.ContractVersion != h.contract {
		h.writeError(w, auth, request.InvocationID, http.StatusBadRequest,
			"invalid_request", "invalid invocation envelope")
		return
	}
	if r.Header.Get("X-DW-Invocation-Id") != request.InvocationID ||
		r.Header.Get("X-DW-Capability-Id") != request.CapabilityID ||
		r.Header.Get("X-DW-Contract-Version") != request.ContractVersion ||
		r.Header.Get("X-DW-Provider-Id") != h.providerID {
		h.writeError(w, auth, request.InvocationID, http.StatusBadRequest,
			"invalid_request", "headers do not match the envelope")
		return
	}
	hash := sha256.Sum256(body)
	reservation := h.replay.reserve(request.InvocationID, hash, h.now())
	if reservation.conflict {
		h.writeError(w, auth, request.InvocationID, http.StatusConflict,
			"invalid_request", "invocation id was reused with a different body")
		return
	}
	if reservation.wait != nil {
		select {
		case <-reservation.wait:
		case <-time.After(14 * time.Second):
			h.writeError(w, auth, request.InvocationID, http.StatusConflict,
				"invalid_request", "an identical invocation is still in progress")
			return
		}
		reservation = h.replay.reserve(request.InvocationID, hash, h.now())
		if reservation.conflict {
			h.writeError(w, auth, request.InvocationID, http.StatusConflict,
				"invalid_request", "invocation id was reused with a different body")
			return
		}
		if reservation.wait != nil {
			h.writeError(w, auth, request.InvocationID, http.StatusConflict,
				"invalid_request", "an identical invocation is still in progress")
			return
		}
	}
	if reservation.hit {
		h.writeResponse(w, auth, request.InvocationID, reservation.status, reservation.body)
		return
	}
	entry := reservation.entry

	var input structureInput
	if err := decodeStrict(request.Input, &input); err != nil {
		h.respond(w, entry, auth, http.StatusBadRequest,
			mustErrorBody(request.InvocationID, "invalid_request", "input is invalid"), 0, "invalid_request")
		return
	}
	if !uuidV7Pattern.MatchString(input.ObjectID) || (input.Format != "plain_text" && input.Format != "markdown") {
		h.respond(w, entry, auth, http.StatusBadRequest,
			mustErrorBody(request.InvocationID, "invalid_request", "input is invalid"), 0, "invalid_request")
		return
	}
	if len([]rune(input.Text)) < 1 || len([]rune(input.Text)) > maxTextRunes {
		h.respond(w, entry, auth, http.StatusBadRequest,
			mustErrorBody(request.InvocationID, "invalid_request", "input text is invalid"), 0, "invalid_request")
		return
	}

	started := h.now()
	h.executions.Add(1)
	output, err := structureText(input.Text, input.Format)
	if err != nil {
		h.respond(w, entry, auth, http.StatusInternalServerError,
			mustErrorBody(request.InvocationID, "capability_failed", "structure failed"), 0, "capability_failed")
		return
	}
	duration := int(h.now().Sub(started).Milliseconds())
	if duration < 0 {
		duration = 0
	}
	response := successResponse{
		InvocationID: request.InvocationID, CapabilityID: capabilityID,
		ContractVersion: h.contract, Status: "succeeded", Output: output,
		DurationMS: duration,
	}
	encoded, err := json.Marshal(response)
	if err != nil || len(encoded) > maxOutputBytes {
		h.respond(w, entry, auth, http.StatusInternalServerError,
			mustErrorBody(request.InvocationID, "capability_failed", "output exceeds the limit"), 0, "capability_failed")
		return
	}
	h.respond(w, entry, auth, http.StatusOK, encoded, duration, "")
}

// looseInvocationID recovers a syntactically valid invocation id after a
// strict envelope decode failed, so a signed error can still be bound to the
// caller's request. It never accepts an id for execution.
func looseInvocationID(body []byte) string {
	var value struct {
		InvocationID string `json:"invocation_id"`
	}
	_ = json.Unmarshal(body, &value)
	if uuidV7Pattern.MatchString(value.InvocationID) {
		return value.InvocationID
	}
	return ""
}

func (h *handler) respond(w http.ResponseWriter, entry *replayEntry, auth *responseAuth,
	status int, body []byte, durationMS int, errorCode string) {
	h.replay.complete(entry, status, body, h.now())
	invocationID := ""
	if entry != nil {
		invocationID = entry.invocationID
	}
	if errorCode != "" {
		log.Printf("capability_provider_invoke invocation_id=%s status=failed error_code=%s duration_ms=%d",
			invocationID, errorCode, durationMS)
	} else {
		log.Printf("capability_provider_invoke invocation_id=%s status=succeeded duration_ms=%d",
			invocationID, durationMS)
	}
	h.writeResponse(w, auth, invocationID, status, body)
}

// verifySignature returns the accepted key so a v2 response can be signed
// with the same key the caller used. An explicit X-DW-Key-Id selects exactly
// one active key; a request without it is tried against every active key for
// legacy compatibility.
func (h *handler) verifySignature(r *http.Request, body []byte) (providerKey, error) {
	timestamp := r.Header.Get("X-DW-Timestamp")
	signature := r.Header.Get("X-DW-Signature")
	if timestamp == "" || signature == "" || signature != strings.ToLower(signature) {
		return providerKey{}, errors.New("missing signature")
	}
	seconds, err := strconv.ParseInt(timestamp, 10, 64)
	if err != nil {
		return providerKey{}, errors.New("invalid timestamp")
	}
	skew := h.now().Sub(time.Unix(seconds, 0))
	if skew < 0 {
		skew = -skew
	}
	if skew > maxSkewSeconds*time.Second {
		return providerKey{}, errors.New("timestamp outside the replay window")
	}
	provided, err := hex.DecodeString(signature)
	if err != nil || len(provided) != sha256.Size {
		return providerKey{}, errors.New("invalid signature encoding")
	}
	requestedID := r.Header.Get("X-DW-Key-Id")
	candidates := make([]providerKey, 0, len(h.keys))
	for _, key := range h.keys {
		if !key.active(h.now()) {
			continue
		}
		if requestedID != "" && key.id != requestedID {
			continue
		}
		candidates = append(candidates, key)
	}
	if len(candidates) == 0 {
		return providerKey{}, errors.New("unknown or expired key id")
	}
	for _, key := range candidates {
		mac := hmac.New(sha256.New, key.secret)
		mac.Write([]byte(timestamp))
		mac.Write([]byte("."))
		mac.Write(body)
		if subtle.ConstantTimeCompare(provided, mac.Sum(nil)) == 1 {
			return key, nil
		}
	}
	return providerKey{}, errors.New("signature mismatch")
}

func decodeStrict(raw []byte, destination any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); err == nil {
		return errors.New("more than one JSON value")
	} else if !errors.Is(err, io.EOF) {
		return err
	}
	return nil
}

func (h *handler) writeError(w http.ResponseWriter, auth *responseAuth, invocationID string,
	status int, code, message string) {
	body, _ := json.Marshal(errorResponse{
		InvocationID: invocationID,
		Error:        providerError{Code: code, Message: message},
	})
	h.writeResponse(w, auth, invocationID, status, body)
}

// writeResponse adds the v2 response signature when the request negotiated
// it. The signature binds the response to the request timestamp, invocation
// id, HTTP status, and exact body bytes.
func (h *handler) writeResponse(w http.ResponseWriter, auth *responseAuth, invocationID string, status int, body []byte) {
	if auth != nil && auth.enabled {
		w.Header().Set("X-DW-Response-Signature-Version", responseSignatureVersion)
		w.Header().Set("X-DW-Response-Signature",
			signProviderResponse(auth.key.secret, status, auth.timestamp, invocationID, body))
		w.Header().Set("X-DW-Key-Id", auth.key.id)
	}
	writeRaw(w, status, body)
}

// signProviderResponse mirrors the Broker's v2 profile independently: HMAC-
// SHA256(secret, "v2\n" + status + "\n" + timestamp + "\n" + invocation_id +
// "\n" + SHA-256(body) hex).
func signProviderResponse(secret []byte, status int, timestamp, invocationID string, body []byte) string {
	bodyHash := sha256.Sum256(body)
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(responseSignatureVersion))
	mac.Write([]byte("\n"))
	mac.Write([]byte(strconv.Itoa(status)))
	mac.Write([]byte("\n"))
	mac.Write([]byte(timestamp))
	mac.Write([]byte("\n"))
	mac.Write([]byte(invocationID))
	mac.Write([]byte("\n"))
	mac.Write([]byte(hex.EncodeToString(bodyHash[:])))
	return hex.EncodeToString(mac.Sum(nil))
}

func mustErrorBody(invocationID, code, message string) []byte {
	body, _ := json.Marshal(errorResponse{
		InvocationID: invocationID,
		Error:        providerError{Code: code, Message: message},
	})
	return body
}

func writeRaw(w http.ResponseWriter, status int, body []byte) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_, _ = w.Write(body)
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	body, _ := json.Marshal(value)
	writeRaw(w, status, body)
}

// replayCache is a bounded, time-windowed LRU keyed by invocation_id. A new
// entry is reserved before execution, so concurrent duplicates wait for the
// first result instead of executing twice.
type replayCache struct {
	mu       sync.Mutex
	capacity int
	entries  map[string]*replayEntry
	order    *list.List
}

type replayEntry struct {
	invocationID string
	hash         [32]byte
	status       int
	body         []byte
	expiresAt    time.Time
	element      *list.Element
	done         chan struct{}
	complete     bool
}

type replayReservation struct {
	entry    *replayEntry
	wait     <-chan struct{}
	hit      bool
	conflict bool
	status   int
	body     []byte
}

func newReplayCache(capacity int) *replayCache {
	if capacity < 1 {
		capacity = 1
	}
	return &replayCache{capacity: capacity, entries: map[string]*replayEntry{}, order: list.New()}
}

func (c *replayCache) reserve(invocationID string, hash [32]byte, now time.Time) replayReservation {
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.entries[invocationID]
	if ok && !entry.expiresAt.After(now) {
		c.order.Remove(entry.element)
		delete(c.entries, invocationID)
		ok = false
	}
	if ok {
		if entry.hash != hash {
			return replayReservation{conflict: true}
		}
		if !entry.complete {
			return replayReservation{wait: entry.done}
		}
		c.order.MoveToFront(entry.element)
		return replayReservation{
			hit: true, status: entry.status,
			body: append([]byte(nil), entry.body...),
		}
	}
	entry = &replayEntry{
		invocationID: invocationID, hash: hash,
		expiresAt: now.Add(replayWindow), done: make(chan struct{}),
	}
	entry.element = c.order.PushFront(invocationID)
	c.entries[invocationID] = entry
	c.evictLocked()
	return replayReservation{entry: entry}
}

func (c *replayCache) complete(entry *replayEntry, status int, body []byte, now time.Time) {
	if entry == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if entry.complete {
		return
	}
	entry.status = status
	entry.body = append([]byte(nil), body...)
	entry.expiresAt = now.Add(replayWindow)
	entry.complete = true
	close(entry.done)
	if entry.element != nil {
		c.order.MoveToFront(entry.element)
	}
}

func (c *replayCache) evictLocked() {
	for len(c.entries) > c.capacity {
		oldest := c.order.Back()
		if oldest == nil {
			return
		}
		key := oldest.Value.(string)
		entry := c.entries[key]
		if entry != nil && !entry.complete {
			return
		}
		c.order.Remove(oldest)
		delete(c.entries, key)
	}
}
