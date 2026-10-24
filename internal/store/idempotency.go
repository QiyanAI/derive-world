// SPDX-License-Identifier: Apache-2.0
package store

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

var (
	// ErrIdempotencyConflict means the same key was used with a different
	// request hash.
	ErrIdempotencyConflict = errors.New("idempotency key was used for a different request")
	// ErrRequestInProgress means another request still holds the lease.
	ErrRequestInProgress = errors.New("an identical request is already in progress")
)

var idempotencyKeyPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{16,128}$`)

// ValidIdempotencyKey reports whether the client-provided key matches the
// frozen format.
func ValidIdempotencyKey(key string) bool {
	return idempotencyKeyPattern.MatchString(key)
}

// Idempotency stores two-phase reservations in idempotency_keys. It is owned
// exclusively by store.
type Idempotency struct {
	pool interface {
		Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
		QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	}
}

// NewIdempotency returns the store-owned idempotency component.
func (s *Store) Idempotency() *Idempotency {
	return &Idempotency{pool: s.pool}
}

// ClaimResult is the outcome of the reservation step.
type ClaimResult struct {
	Replay     bool
	Reserved   bool
	StatusCode int
	Body       []byte
	Truncated  bool
	LeaseUntil time.Time
}

// Claim reserves the key or returns the completed first response. The caller
// must have already performed LoadResource, Authorize, and ValidateSchema
// (G00 contract §A.7.1).
func (i *Idempotency) Claim(ctx context.Context, actorID uuid.UUID, route, key string, requestHash []byte) (*ClaimResult, error) {
	if !ValidIdempotencyKey(key) {
		return nil, errors.New("invalid idempotency key format")
	}
	if len(requestHash) != 32 {
		return nil, fmt.Errorf("request hash must be 32 bytes, got %d", len(requestHash))
	}

	tag, err := i.pool.Exec(ctx, `
INSERT INTO idempotency_keys (
    actor_user_id, route, key, request_hash, lease_until, expires_at
) VALUES ($1,$2,$3,$4, now() + interval '10 minutes', now() + interval '24 hours')
ON CONFLICT (actor_user_id, route, key) DO NOTHING`,
		actorID.String(), route, key, requestHash)
	if err != nil {
		return nil, err
	}
	if tag.RowsAffected() == 1 {
		var leaseUntil time.Time
		if err := i.pool.QueryRow(ctx, `
SELECT lease_until FROM idempotency_keys
 WHERE actor_user_id = $1 AND route = $2 AND key = $3`,
			actorID.String(), route, key).Scan(&leaseUntil); err != nil {
			return nil, err
		}
		return &ClaimResult{Reserved: true, LeaseUntil: leaseUntil}, nil
	}

	var (
		storedHash []byte
		statusCode *int
		body       []byte
		truncated  bool
		inProgress bool
		expired    bool
	)
	err = i.pool.QueryRow(ctx, `
SELECT request_hash, status_code, response_body, response_truncated,
       (status_code IS NULL AND lease_until > now()) AS in_progress,
       (expires_at <= now()) AS expired
  FROM idempotency_keys
 WHERE actor_user_id = $1 AND route = $2 AND key = $3`,
		actorID.String(), route, key,
	).Scan(&storedHash, &statusCode, &body, &truncated, &inProgress, &expired)
	if err != nil {
		return nil, err
	}

	if statusCode != nil && !expired {
		if !bytes.Equal(storedHash, requestHash) {
			return nil, ErrIdempotencyConflict
		}
		if body == nil {
			body = []byte("{}")
		}
		return &ClaimResult{
			Replay:     true,
			StatusCode: *statusCode,
			Body:       body,
			Truncated:  truncated,
		}, nil
	}
	if statusCode == nil {
		if !bytes.Equal(storedHash, requestHash) {
			return nil, ErrIdempotencyConflict
		}
		if inProgress {
			return nil, ErrRequestInProgress
		}
		// The previous holder crashed and its ten-minute lease expired.
		// Exactly one contender wins the conditional takeover.
		tag, err = i.pool.Exec(ctx, `
UPDATE idempotency_keys
   SET lease_until = now() + interval '10 minutes',
       expires_at  = now() + interval '24 hours'
 WHERE actor_user_id = $1 AND route = $2 AND key = $3
   AND request_hash = $4
   AND status_code IS NULL
   AND lease_until <= now()`,
			actorID.String(), route, key, requestHash)
		if err != nil {
			return nil, err
		}
		if tag.RowsAffected() == 0 {
			return nil, ErrRequestInProgress
		}
		var leaseUntil time.Time
		if err := i.pool.QueryRow(ctx, `
SELECT lease_until FROM idempotency_keys
 WHERE actor_user_id = $1 AND route = $2 AND key = $3`,
			actorID.String(), route, key).Scan(&leaseUntil); err != nil {
			return nil, err
		}
		return &ClaimResult{Reserved: true, LeaseUntil: leaseUntil}, nil
	}

	// A completed record older than 24 hours is treated as a new request,
	// even when the new body differs from the old one.
	tag, err = i.pool.Exec(ctx, `
UPDATE idempotency_keys
   SET request_hash = $4,
       status_code = NULL,
       response_body = NULL,
       response_truncated = false,
       completed_at = NULL,
       created_at = now(),
       lease_until = now() + interval '10 minutes',
       expires_at  = now() + interval '24 hours'
 WHERE actor_user_id = $1 AND route = $2 AND key = $3
   AND status_code IS NOT NULL
   AND expires_at <= now()`,
		actorID.String(), route, key, requestHash)
	if err != nil {
		return nil, err
	}
	if tag.RowsAffected() == 0 {
		return nil, ErrRequestInProgress
	}
	var leaseUntil time.Time
	if err := i.pool.QueryRow(ctx, `
SELECT lease_until FROM idempotency_keys
 WHERE actor_user_id = $1 AND route = $2 AND key = $3`,
		actorID.String(), route, key).Scan(&leaseUntil); err != nil {
		return nil, err
	}
	return &ClaimResult{Reserved: true, LeaseUntil: leaseUntil}, nil
}

// Complete writes the first response into the reservation. It must run in the
// same transaction as the business writes when the request succeeds.
func (i *Idempotency) Complete(ctx context.Context, tx pgx.Tx, actorID uuid.UUID, route, key string, leaseUntil time.Time, statusCode int, body []byte) error {
	truncated := false
	if len(body) > 256*1024 {
		body = []byte(`{"truncated":true}`)
		truncated = true
		statusCode = 200
	}
	if !json.Valid(body) {
		return errors.New("idempotency response body must be valid JSON")
	}
	tag, err := tx.Exec(ctx, `
UPDATE idempotency_keys
   SET status_code = $4,
       response_body = $5,
       response_truncated = $6,
       completed_at = now(),
       expires_at = now() + interval '24 hours'
 WHERE actor_user_id = $1 AND route = $2 AND key = $3
   AND status_code IS NULL
   AND lease_until = $7`,
		actorID.String(), route, key, statusCode, body, truncated, leaseUntil)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return errors.New("idempotency reservation was not found or was already completed")
	}
	return nil
}

// Cancel releases an unused reservation before any business side effect. It is
// used when a pre-execution admission check (for example a model budget) fails;
// no cached response is written. The table is update-only for the runtime role,
// so cancellation expires the lease rather than deleting the row. A retry with
// the same key can then take over the reservation immediately.
func (i *Idempotency) Cancel(ctx context.Context, actorID uuid.UUID, route, key string, leaseUntil time.Time) error {
	_, err := i.pool.Exec(ctx, `
UPDATE idempotency_keys
   SET lease_until = now() - interval '1 second'
 WHERE actor_user_id = $1 AND route = $2 AND key = $3
   AND status_code IS NULL AND lease_until = $4`,
		actorID.String(), route, key, leaseUntil)
	return err
}

// CompleteStandalone records a deterministic failure after the business
// transaction rolled back. It must not be used for successful business writes.
func (i *Idempotency) CompleteStandalone(ctx context.Context, actorID uuid.UUID, route, key string, leaseUntil time.Time, statusCode int, body []byte) error {
	truncated := false
	if len(body) > 256*1024 {
		body = []byte(`{"truncated":true}`)
		truncated = true
		statusCode = 200
	}
	if !json.Valid(body) {
		body = []byte(`{}`)
	}
	tag, err := i.pool.Exec(ctx, `
UPDATE idempotency_keys
   SET status_code = $4,
       response_body = $5,
       response_truncated = $6,
       completed_at = now(),
       expires_at = now() + interval '24 hours'
 WHERE actor_user_id = $1 AND route = $2 AND key = $3
   AND status_code IS NULL
   AND lease_until = $7`,
		actorID.String(), route, key, statusCode, body, truncated, leaseUntil)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return errors.New("idempotency reservation was not found or was already completed")
	}
	return nil
}

// RequestHash computes SHA-256(method + "\n" + actualPath + "\n" + canonical JSON).
func RequestHash(method, actualPath string, body []byte) ([]byte, error) {
	canonical, err := CanonicalJSON(body)
	if err != nil {
		return nil, err
	}
	digest := sha256.New()
	digest.Write([]byte(method))
	digest.Write([]byte("\n"))
	digest.Write([]byte(actualPath))
	digest.Write([]byte("\n"))
	digest.Write(canonical)
	return digest.Sum(nil), nil
}

// CanonicalJSON parses JSON and re-encodes it with sorted object keys and no
// insignificant whitespace. An empty body canonicalizes to null.
func CanonicalJSON(body []byte) ([]byte, error) {
	value, err := canonicalValue(body)
	if err != nil {
		return nil, err
	}
	if value == nil {
		return []byte("null"), nil
	}
	return json.Marshal(value)
}

// CanonicalJSONSize parses one JSON document, applies the same canonical
// number handling as CanonicalJSON, and returns CompactJSONSize of the value.
// It is used for business size caps whose frozen definition is on the jsonb
// text representation; it does not change the canonical bytes used for hashes
// or storage.
func CanonicalJSONSize(body []byte) (int, error) {
	value, err := canonicalValue(body)
	if err != nil {
		return 0, err
	}
	if value == nil {
		return len("null"), nil
	}
	return CompactJSONSize(value)
}

func canonicalValue(body []byte) (any, error) {
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) == 0 {
		return nil, nil
	}
	decoder := json.NewDecoder(bytes.NewReader(trimmed))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	var extra any
	err := decoder.Decode(&extra)
	if err == nil {
		return nil, errors.New("request body must contain one JSON value")
	}
	if !errors.Is(err, io.EOF) {
		return nil, err
	}
	return canonicalizeNumbers(value), nil
}

// canonicalNumber emits a pre-normalized JSON number without re-quoting.
type canonicalNumber string

func (n canonicalNumber) MarshalJSON() ([]byte, error) {
	return []byte(n), nil
}

func canonicalizeNumbers(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		for key, nested := range typed {
			typed[key] = canonicalizeNumbers(nested)
		}
		return typed
	case []any:
		for index, nested := range typed {
			typed[index] = canonicalizeNumbers(nested)
		}
		return typed
	case json.Number:
		return canonicalNumber(canonicalNumberString(string(typed)))
	default:
		return value
	}
}

// canonicalNumberString reduces a valid JSON number to a unique decimal form:
// 1, 1.0, 1e0 -> 1; 0.10 -> 0.1; 100e-2 -> 1. Extreme exponents keep their
// original spelling rather than allocating an unbounded string.
func canonicalNumberString(raw string) string {
	negative := strings.HasPrefix(raw, "-")
	if negative {
		raw = raw[1:]
	}
	mantissa := raw
	exponent := 0
	if index := strings.IndexAny(raw, "eE"); index >= 0 {
		mantissa = raw[:index]
		parsed, err := strconv.Atoi(raw[index+1:])
		if err != nil || parsed > 10000 || parsed < -10000 {
			return rawWithSign(negative, raw)
		}
		exponent = parsed
	}
	intPart := mantissa
	fracPart := ""
	if index := strings.IndexByte(mantissa, '.'); index >= 0 {
		intPart = mantissa[:index]
		fracPart = mantissa[index+1:]
	}
	digits := strings.TrimLeft(intPart+fracPart, "0")
	if digits == "" {
		return "0"
	}
	scale := len(fracPart) - exponent
	for scale > 0 && strings.HasSuffix(digits, "0") {
		digits = strings.TrimSuffix(digits, "0")
		scale--
	}
	var canonical string
	switch {
	case scale <= 0:
		canonical = digits + strings.Repeat("0", -scale)
	case scale >= len(digits):
		canonical = "0." + strings.Repeat("0", scale-len(digits)) + digits
	default:
		canonical = digits[:len(digits)-scale] + "." + digits[len(digits)-scale:]
	}
	return rawWithSign(negative, canonical)
}

func rawWithSign(negative bool, value string) string {
	if negative && value != "0" {
		return "-" + value
	}
	return value
}
