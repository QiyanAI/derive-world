// SPDX-License-Identifier: Apache-2.0

// Package assets owns bounded media files that a world may display.
//
// Object state never contains file bytes. It contains short asset ids; this
// package stores validated bytes in a content-addressed directory, records
// metadata, and authorizes every read through an active world (or publication)
// reference. It never executes uploaded content and never serves a file type
// outside the explicit media allowlist.
package assets

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/QiyanAI/derive-world/internal/audit"
)

// Kind is one closed media category. Video and audio are intentionally
// separate even when they share a container, because the browser element and
// the capability that renders them differ.
type Kind string

const (
	KindImage Kind = "image"
	KindVideo Kind = "video"
	KindAudio Kind = "audio"
	KindModel Kind = "model"
)

// Per-kind upload ceilings. The database repeats the overall 40 MiB ceiling.
const (
	MaxImageBytes      = 8 << 20
	MaxVideoBytes      = 40 << 20
	MaxAudioBytes      = 16 << 20
	MaxModelBytes      = 32 << 20
	MaxWorldBytes      = 64 << 20
	MaxUserDailyBytes  = 256 << 20
	MaxAssetsPerWorld  = 40
	MaxOriginalNameLen = 120
)

// Domain errors. The HTTP layer maps these to stable API codes.
var (
	ErrNotFound        = errors.New("asset not found")
	ErrForbidden       = errors.New("asset forbidden")
	ErrInvalidKind     = errors.New("invalid asset kind")
	ErrTooLarge        = errors.New("asset exceeds the size limit")
	ErrUnsupportedType = errors.New("unsupported asset type")
	ErrQuota           = errors.New("asset quota exceeded")
	ErrInvalidContent  = errors.New("invalid asset content")
	ErrInUse           = errors.New("asset is referenced by the current world state")
	ErrUnavailable     = errors.New("asset storage unavailable")
)

// Asset is the public metadata of one stored file. It never contains the
// filesystem path.
type Asset struct {
	ID        string    `json:"id"`
	Kind      Kind      `json:"kind"`
	MIME      string    `json:"mime"`
	ByteSize  int64     `json:"byte_size"`
	Width     int       `json:"width,omitempty"`
	Height    int       `json:"height,omitempty"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
	Checksum  []byte    `json:"-"`
}

// Generator is the extension point for future server-side media generation.
// A generator returns already-validated bytes for one supported kind; the
// service still applies the same size, quota, audit and authorization path as
// an upload. No generator is registered by default.
type Generator interface {
	Name() string
	Kinds() []Kind
	Generate(ctx context.Context, request GenerateRequest) (Generated, error)
}

// GenerateRequest is deliberately small and provider-neutral.
type GenerateRequest struct {
	Kind     Kind           `json:"kind"`
	Provider string         `json:"provider,omitempty"`
	Prompt   string         `json:"prompt,omitempty"`
	Params   map[string]any `json:"params,omitempty"`
}

// Generated is one generated file before storage.
type Generated struct {
	Bytes    []byte
	MIME     string
	Width    int
	Height   int
	Name     string
	Metadata map[string]any
}

// Service owns asset storage and metadata.
type Service struct {
	db        *pgxpool.Pool
	audit     *audit.Writer
	root      string
	blobRoot  string
	tempRoot  string
	generator Generator
	imageGate chan struct{}
}

// New creates the asset service and its private directory layout. root must
// not live under the public web root; the composition root enforces that.
func New(db *pgxpool.Pool, auditWriter *audit.Writer, root string) (*Service, error) {
	if db == nil {
		return nil, errors.New("assets: database is required")
	}
	root = strings.TrimSpace(root)
	if root == "" {
		return nil, errors.New("assets: storage directory is required")
	}
	absolute, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("assets: resolve storage directory: %w", err)
	}
	service := &Service{
		db:        db,
		audit:     auditWriter,
		root:      absolute,
		blobRoot:  filepath.Join(absolute, "blobs"),
		tempRoot:  filepath.Join(absolute, "tmp"),
		imageGate: make(chan struct{}, 2),
	}
	for _, directory := range []string{service.blobRoot, service.tempRoot} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			return nil, fmt.Errorf("assets: create storage directory: %w", err)
		}
	}
	return service, nil
}

// SetGenerator installs the optional generation provider. It has no effect on
// uploads and is never required.
func (s *Service) SetGenerator(generator Generator) {
	s.generator = generator
}

// Generator returns the configured generator, if any.
func (s *Service) Generator() Generator {
	return s.generator
}

// SweepTemp removes abandoned upload scratch files older than the grace
// period. It never touches the content-addressed blob set or any metadata.
func (s *Service) SweepTemp(maxAge time.Duration) {
	entries, err := os.ReadDir(s.tempRoot)
	if err != nil {
		return
	}
	cutoff := time.Now().Add(-maxAge)
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		info, err := entry.Info()
		if err != nil || info.ModTime().After(cutoff) {
			continue
		}
		_ = os.Remove(filepath.Join(s.tempRoot, entry.Name()))
	}
}

// LimitFor returns the per-kind byte ceiling.
func LimitFor(kind Kind) (int64, bool) {
	switch kind {
	case KindImage:
		return MaxImageBytes, true
	case KindVideo:
		return MaxVideoBytes, true
	case KindAudio:
		return MaxAudioBytes, true
	case KindModel:
		return MaxModelBytes, true
	default:
		return 0, false
	}
}

// ParseKind accepts only the four public media categories.
func ParseKind(value string) (Kind, bool) {
	switch Kind(strings.ToLower(strings.TrimSpace(value))) {
	case KindImage:
		return KindImage, true
	case KindVideo:
		return KindVideo, true
	case KindAudio:
		return KindAudio, true
	case KindModel:
		return KindModel, true
	default:
		return "", false
	}
}

// Save writes one already-validated generated file through the normal
// upload/quota/audit path. It is used by a configured Generator.
func (s *Service) Save(ctx context.Context, actorID, objectID uuid.UUID,
	kind Kind, name string, content io.Reader, requestID string, ipHash []byte) (*Asset, error) {
	return s.Upload(ctx, actorID, objectID, kind, name, content, requestID, ipHash)
}

func sanitizeName(name string) string {
	name = strings.TrimSpace(name)
	if slash := strings.LastIndexAny(name, `/\`); slash >= 0 {
		name = name[slash+1:]
	}
	var builder strings.Builder
	for _, r := range name {
		if r < 0x20 || r == 0x7f || r == '"' || r == '\'' {
			continue
		}
		builder.WriteRune(r)
		if builder.Len() >= MaxOriginalNameLen {
			break
		}
	}
	cleaned := strings.TrimSpace(builder.String())
	if cleaned == "" {
		return "asset"
	}
	return cleaned
}
