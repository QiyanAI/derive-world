// SPDX-License-Identifier: Apache-2.0

// Package publication owns the opt-in public projection of one object version,
// contributor consent, anonymous discovery, and public derivation. Anonymous
// reads never touch the private object read path; they only see the stored,
// sanitized snapshot.
package publication

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/QiyanAI/derive-world/internal/audit"
	"github.com/QiyanAI/derive-world/internal/derive"
	"github.com/QiyanAI/derive-world/internal/objects"
	"github.com/QiyanAI/derive-world/internal/store"
)

var (
	ErrNotFound       = errors.New("publication not found")
	ErrForbidden      = errors.New("publication forbidden")
	ErrConflict       = errors.New("publication conflict")
	ErrInvalidRequest = errors.New("invalid publication request")
)

const maxTitleRunes = 200

var allowedLicenses = map[string]bool{
	"all_rights_reserved": true,
	"cc_by_4.0":           true,
	"cc_by_sa_4.0":        true,
}

// Publication is the publisher-facing projection.
type Publication struct {
	ID                   string          `json:"id"`
	ObjectID             string          `json:"object_id"`
	SourceVersion        int             `json:"source_version"`
	PublisherID          string          `json:"publisher_id"`
	PublisherName        string          `json:"publisher_name"`
	Title                string          `json:"title"`
	Summary              string          `json:"summary"`
	ObjectType           string          `json:"object_type"`
	PresentationTemplate string          `json:"presentation_template"`
	Snapshot             json.RawMessage `json:"snapshot,omitempty"`
	Status               string          `json:"status"`
	DeriveAllowed        bool            `json:"derive_allowed"`
	License              string          `json:"license"`
	CreatedAt            time.Time       `json:"created_at"`
	UpdatedAt            time.Time       `json:"updated_at"`
	RevokedAt            *time.Time      `json:"revoked_at"`
}

// Consent is one contributor's pending publication approval.
type Consent struct {
	ID            string     `json:"id"`
	PublicationID string     `json:"publication_id"`
	ObjectID      string     `json:"object_id"`
	Title         string     `json:"title"`
	PublisherName string     `json:"publisher_name"`
	Status        string     `json:"status"`
	CreatedAt     time.Time  `json:"created_at"`
	RespondedAt   *time.Time `json:"responded_at"`
}

// CreateInput is the strict publication request.
type CreateInput struct {
	SourceVersion *int   `json:"source_version"`
	DeriveAllowed bool   `json:"derive_allowed"`
	License       string `json:"license"`
}

// PublicWorld is the anonymous snapshot projection. It never contains
// shared_context, memberships, AI data, or credentials.
type PublicWorld struct {
	ID                   string          `json:"id"`
	Title                string          `json:"title"`
	Summary              string          `json:"summary"`
	ObjectType           string          `json:"object_type"`
	PresentationTemplate string          `json:"presentation_template"`
	SourceVersion        int             `json:"source_version"`
	PublisherName        string          `json:"publisher_name"`
	Snapshot             json.RawMessage `json:"snapshot,omitempty"`
	DeriveAllowed        bool            `json:"derive_allowed"`
	License              string          `json:"license"`
	CreatedAt            time.Time       `json:"created_at"`
}

// PublicCursor is the keyset cursor for the public discovery list.
type PublicCursor struct {
	CreatedAt time.Time
	ID        uuid.UUID
}

// PublicSource is the public parent relation of a published object.
type PublicSource struct {
	PublicationID   string   `json:"publication_id"`
	Title           string   `json:"title"`
	SourceVersion   int      `json:"source_version"`
	InheritedFields []string `json:"inherited_fields"`
}

// PublicBranch is an actively published direct derivation.
type PublicBranch struct {
	PublicationID string `json:"publication_id"`
	Title         string `json:"title"`
	SourceVersion int    `json:"source_version"`
}

// PublicLineage is the public-only lineage projection.
type PublicLineage struct {
	Source  *PublicSource  `json:"source"`
	Derived []PublicBranch `json:"derived"`
}

// Service is the controlled Publication write path.
type Service struct {
	db      *pgxpool.Pool
	objects *objects.Service
	audit   *audit.Writer
	idem    *store.Idempotency
}

// NewService assembles the Publication service.
func NewService(db *pgxpool.Pool, objectService *objects.Service, auditWriter *audit.Writer, idem *store.Idempotency) *Service {
	return &Service{db: db, objects: objectService, audit: auditWriter, idem: idem}
}

// Create publishes one object version. It requires object ownership and manage
// permission. Other contributors must approve before the publication becomes
// publicly visible.
func (s *Service) Create(ctx context.Context, publisherID, objectID uuid.UUID, input CreateInput,
	route, idemKey string, leaseUntil time.Time, requestID string, ipHash []byte) (*Publication, error) {
	if !allowedLicenses[input.License] {
		return nil, fmt.Errorf("%w: license", ErrInvalidRequest)
	}
	tx, err := s.db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	object, permissions, err := s.objects.LoadForDerive(ctx, tx, publisherID, objectID)
	if err != nil {
		return nil, err
	}
	if object.OwnerID != publisherID || !objects.HasPermission(permissions, objects.PermManage) {
		return nil, ErrForbidden
	}
	sourceVersion := object.Version
	if input.SourceVersion != nil {
		if *input.SourceVersion < 1 || *input.SourceVersion > object.Version {
			return nil, fmt.Errorf("%w: source_version", ErrInvalidRequest)
		}
		sourceVersion = *input.SourceVersion
	}
	sourceState := object.State
	if sourceVersion != object.Version {
		sourceState, err = s.objects.StateAtVersion(ctx, tx, objectID, sourceVersion)
		if err != nil {
			return nil, err
		}
	}
	snapshot, title, summary, err := buildSnapshot(object.ObjectType, object.PresentationTemplate, sourceState)
	if err != nil {
		return nil, err
	}
	contributors, err := contributorIDs(ctx, tx, objectID, sourceVersion, publisherID)
	if err != nil {
		return nil, err
	}
	status := "active"
	if len(contributors) > 0 {
		status = "pending_consent"
	}
	publicationID, err := uuid.NewV7()
	if err != nil {
		return nil, err
	}
	var (
		createdAt time.Time
		updatedAt time.Time
	)
	err = tx.QueryRow(ctx, `
INSERT INTO publications (
    id, object_id, source_version, publisher_id, title, summary, object_type,
    presentation_template, snapshot, status, derive_allowed, license
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9::jsonb,$10,$11,$12)
RETURNING created_at, updated_at`,
		publicationID.String(), objectID.String(), sourceVersion, publisherID.String(),
		title, summary, object.ObjectType, object.PresentationTemplate,
		string(snapshot), status, input.DeriveAllowed, input.License,
	).Scan(&createdAt, &updatedAt)
	if err != nil {
		return nil, mapPublicationError(err)
	}
	for _, contributorID := range contributors {
		consentID, err := uuid.NewV7()
		if err != nil {
			return nil, err
		}
		if _, err := tx.Exec(ctx, `
INSERT INTO publication_consents (id, publication_id, contributor_id)
VALUES ($1,$2,$3)`, consentID.String(), publicationID.String(), contributorID.String()); err != nil {
			return nil, mapPublicationError(err)
		}
	}
	if err := s.writeAudit(ctx, tx, publisherID, audit.ActionPublicationCreate,
		"publication", &publicationID, requestID, ipHash,
		map[string]any{"object_id": objectID.String(), "source_version": sourceVersion,
			"status": status, "contributors": len(contributors)}); err != nil {
		return nil, err
	}
	if status == "active" {
		if err := s.writeAudit(ctx, tx, publisherID, audit.ActionPublicationActivate,
			"publication", &publicationID, requestID, ipHash, nil); err != nil {
			return nil, err
		}
	}
	var publisherName string
	if err := tx.QueryRow(ctx, `SELECT display_name FROM users WHERE id = $1`,
		publisherID.String()).Scan(&publisherName); err != nil {
		return nil, err
	}
	publication := &Publication{
		ID: publicationID.String(), ObjectID: objectID.String(),
		SourceVersion: sourceVersion, PublisherID: publisherID.String(),
		PublisherName: publisherName, Title: title, Summary: summary,
		ObjectType: object.ObjectType, PresentationTemplate: object.PresentationTemplate,
		Status: status, DeriveAllowed: input.DeriveAllowed, License: input.License,
		CreatedAt: createdAt, UpdatedAt: updatedAt,
	}
	body, err := json.Marshal(publication)
	if err != nil {
		return nil, err
	}
	if s.idem != nil {
		if err := s.idem.Complete(ctx, tx, publisherID, route, idemKey, leaseUntil, 201, body); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return publication, nil
}

// ListForObject returns publication records visible to an authorized reader.
func (s *Service) ListForObject(ctx context.Context, actorID, objectID uuid.UUID) ([]Publication, error) {
	if _, err := s.objects.LoadForActor(ctx, actorID, objectID); err != nil {
		return nil, err
	}
	rows, err := s.db.Query(ctx, `
SELECT p.id, p.object_id, p.source_version, p.publisher_id, u.display_name,
       p.title, p.summary, p.object_type, p.presentation_template, p.status,
       p.derive_allowed, p.license, p.created_at, p.updated_at, p.revoked_at
  FROM publications p
  JOIN users u ON u.id = p.publisher_id
 WHERE p.object_id = $1
 ORDER BY p.created_at DESC, p.id DESC`, objectID.String())
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	publications := make([]Publication, 0)
	for rows.Next() {
		var (
			publication Publication
			id          string
			objectValue string
			publisherID string
		)
		if err := rows.Scan(&id, &objectValue, &publication.SourceVersion, &publisherID,
			&publication.PublisherName, &publication.Title, &publication.Summary,
			&publication.ObjectType, &publication.PresentationTemplate, &publication.Status,
			&publication.DeriveAllowed, &publication.License, &publication.CreatedAt,
			&publication.UpdatedAt, &publication.RevokedAt); err != nil {
			return nil, err
		}
		publication.ID = id
		publication.ObjectID = objectValue
		publication.PublisherID = publisherID
		publications = append(publications, publication)
	}
	return publications, rows.Err()
}

// Revoke stops new public reads and derivations immediately.
func (s *Service) Revoke(ctx context.Context, actorID, publicationID uuid.UUID, requestID string, ipHash []byte) error {
	tx, err := s.db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var (
		publisherID string
		status      string
	)
	err = tx.QueryRow(ctx, `
SELECT publisher_id, status FROM publications WHERE id = $1 FOR UPDATE`,
		publicationID.String()).Scan(&publisherID, &status)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if publisherID != actorID.String() {
		return ErrNotFound
	}
	if status == "revoked" {
		return tx.Commit(ctx)
	}
	if _, err := tx.Exec(ctx, `
UPDATE publications
   SET status = 'revoked', revoked_at = now(), updated_at = now()
 WHERE id = $1`, publicationID.String()); err != nil {
		return err
	}
	if err := s.writeAudit(ctx, tx, actorID, audit.ActionPublicationRevoke,
		"publication", &publicationID, requestID, ipHash, nil); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// PendingConsents returns publication approvals waiting for the caller.
func (s *Service) PendingConsents(ctx context.Context, userID uuid.UUID) ([]Consent, error) {
	rows, err := s.db.Query(ctx, `
SELECT c.id, p.id, p.object_id, p.title, u.display_name, c.status, c.created_at, c.responded_at
  FROM publication_consents c
  JOIN publications p ON p.id = c.publication_id
  JOIN users u ON u.id = p.publisher_id
 WHERE c.contributor_id = $1 AND c.status = 'pending' AND p.status = 'pending_consent'
 ORDER BY c.created_at DESC, c.id DESC`, userID.String())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	consents := make([]Consent, 0)
	for rows.Next() {
		var (
			consent Consent
			id      string
			pubID   string
			objID   string
		)
		if err := rows.Scan(&id, &pubID, &objID, &consent.Title, &consent.PublisherName,
			&consent.Status, &consent.CreatedAt, &consent.RespondedAt); err != nil {
			return nil, err
		}
		consent.ID = id
		consent.PublicationID = pubID
		consent.ObjectID = objID
		consents = append(consents, consent)
	}
	return consents, rows.Err()
}

// RespondConsent approves or declines one pending request. The publication
// becomes active only when every contributor has approved.
func (s *Service) RespondConsent(ctx context.Context, userID, consentID uuid.UUID, approve bool,
	requestID string, ipHash []byte) (*Consent, error) {
	tx, err := s.db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var (
		publicationID string
		contributorID string
		status        string
	)
	err = tx.QueryRow(ctx, `
SELECT publication_id, contributor_id, status
  FROM publication_consents WHERE id = $1 FOR UPDATE`, consentID.String()).Scan(
		&publicationID, &contributorID, &status)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if contributorID != userID.String() || status != "pending" {
		return nil, ErrNotFound
	}
	var publicationStatus string
	if err := tx.QueryRow(ctx, `
SELECT status FROM publications WHERE id = $1 FOR UPDATE`,
		publicationID).Scan(&publicationStatus); err != nil {
		return nil, err
	}
	if publicationStatus != "pending_consent" {
		return nil, ErrConflict
	}
	newStatus := "declined"
	if approve {
		newStatus = "approved"
	}
	if _, err := tx.Exec(ctx, `
UPDATE publication_consents
   SET status = $2, responded_at = now()
 WHERE id = $1`, consentID.String(), newStatus); err != nil {
		return nil, err
	}
	if !approve {
		if _, err := tx.Exec(ctx, `
UPDATE publications SET status = 'declined', updated_at = now() WHERE id = $1`,
			publicationID); err != nil {
			return nil, err
		}
	} else {
		var pending int
		if err := tx.QueryRow(ctx, `
SELECT count(*) FROM publication_consents
 WHERE publication_id = $1 AND status = 'pending'`, publicationID).Scan(&pending); err != nil {
			return nil, err
		}
		if pending == 0 {
			if _, err := tx.Exec(ctx, `
UPDATE publications SET status = 'active', updated_at = now() WHERE id = $1`,
				publicationID); err != nil {
				return nil, err
			}
			publicationUUID := uuid.MustParse(publicationID)
			if err := s.writeAudit(ctx, tx, userID, audit.ActionPublicationActivate,
				"publication", &publicationUUID, requestID, ipHash, nil); err != nil {
				return nil, err
			}
		}
	}
	if err := s.writeAudit(ctx, tx, userID, audit.ActionPublicationConsent,
		"publication_consent", &consentID, requestID, ipHash,
		map[string]any{"decision": newStatus}); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return &Consent{ID: consentID.String(), PublicationID: publicationID, Status: newStatus,
		RespondedAt: timePtr(time.Now().UTC())}, nil
}

// Report records one authenticated abuse report for an active publication.
// A repeated report by the same user is idempotent and does not duplicate.
func (s *Service) Report(ctx context.Context, reporterID, publicationID uuid.UUID, reason string,
	requestID string, ipHash []byte) error {
	trimmed := strings.TrimSpace(reason)
	if runeLength(trimmed) < 1 || runeLength(reason) > 1000 || strings.ContainsRune(reason, 0) {
		return fmt.Errorf("%w: reason", ErrInvalidRequest)
	}
	tx, err := s.db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var objectID string
	err = tx.QueryRow(ctx, `
SELECT object_id FROM publications WHERE id = $1 AND status = 'active'`,
		publicationID.String()).Scan(&objectID)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	reportID, err := uuid.NewV7()
	if err != nil {
		return err
	}
	command, err := tx.Exec(ctx, `
INSERT INTO publication_reports (id, publication_id, reporter_id, reason)
VALUES ($1,$2,$3,$4)
ON CONFLICT (publication_id, reporter_id) DO NOTHING`,
		reportID.String(), publicationID.String(), reporterID.String(), reason)
	if err != nil {
		return mapPublicationError(err)
	}
	if command.RowsAffected() > 0 {
		if err := s.writeAudit(ctx, tx, reporterID, audit.ActionPublicationReport,
			"publication", &publicationID, requestID, ipHash, nil); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

// PublicList returns active publications ordered newest first.
func (s *Service) PublicList(ctx context.Context, cursor *PublicCursor, limit int) ([]PublicWorld, *PublicCursor, error) {
	query := `
SELECT p.id, p.title, p.summary, p.object_type, p.presentation_template,
       p.source_version, u.display_name, p.derive_allowed, p.license, p.created_at
  FROM publications p
  JOIN users u ON u.id = p.publisher_id
 WHERE p.status = 'active'`
	args := []any{}
	if cursor != nil {
		query += ` AND (p.created_at, p.id) < ($1::timestamptz, $2::uuid)`
		args = append(args, cursor.CreatedAt, cursor.ID.String())
	}
	query += fmt.Sprintf(` ORDER BY p.created_at DESC, p.id DESC LIMIT $%d`, len(args)+1)
	args = append(args, limit+1)

	rows, err := s.db.Query(ctx, query, args...)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()

	items := make([]PublicWorld, 0, limit)
	var (
		count    int
		lastTime time.Time
		lastID   uuid.UUID
	)
	for rows.Next() {
		var (
			world PublicWorld
			id    string
		)
		if err := rows.Scan(&id, &world.Title, &world.Summary, &world.ObjectType,
			&world.PresentationTemplate, &world.SourceVersion, &world.PublisherName,
			&world.DeriveAllowed, &world.License, &world.CreatedAt); err != nil {
			return nil, nil, err
		}
		world.ID = id
		count++
		if count > limit {
			continue
		}
		lastTime = world.CreatedAt
		lastID = uuid.MustParse(world.ID)
		items = append(items, world)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}
	if count > limit {
		return items, &PublicCursor{CreatedAt: lastTime, ID: lastID}, nil
	}
	return items, nil, nil
}

// PublicGet returns one active snapshot. Pending, declined, and revoked
// publications are indistinguishable from missing.
func (s *Service) PublicGet(ctx context.Context, publicationID uuid.UUID) (*PublicWorld, error) {
	var (
		world    PublicWorld
		id       string
		snapshot []byte
	)
	err := s.db.QueryRow(ctx, `
SELECT p.id, p.title, p.summary, p.object_type, p.presentation_template,
       p.source_version, u.display_name, p.snapshot, p.derive_allowed, p.license, p.created_at
  FROM publications p
  JOIN users u ON u.id = p.publisher_id
 WHERE p.id = $1 AND p.status = 'active'`, publicationID.String()).Scan(
		&id, &world.Title, &world.Summary, &world.ObjectType, &world.PresentationTemplate,
		&world.SourceVersion, &world.PublisherName, &snapshot, &world.DeriveAllowed,
		&world.License, &world.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	world.ID = id
	world.Snapshot = snapshot
	return &world, nil
}

// PublicLineage returns only actively published source/branch relations.
func (s *Service) PublicLineage(ctx context.Context, publicationID uuid.UUID) (*PublicLineage, error) {
	var objectID string
	err := s.db.QueryRow(ctx, `
SELECT object_id FROM publications WHERE id = $1 AND status = 'active'`,
		publicationID.String()).Scan(&objectID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	lineage := &PublicLineage{Derived: make([]PublicBranch, 0)}
	var (
		sourcePubID string
		sourceTitle string
		parentVer   int
		fields      []string
	)
	err = s.db.QueryRow(ctx, `
SELECT p2.id, p2.title, d.parent_version, d.inherited_fields
  FROM derivation_edges d
  JOIN publications p2 ON p2.object_id = d.parent_object_id
                      AND p2.source_version = d.parent_version
                      AND p2.status = 'active'
 WHERE d.child_object_id = $1
 ORDER BY p2.created_at DESC
 LIMIT 1`, objectID).Scan(&sourcePubID, &sourceTitle, &parentVer, &fields)
	if err == nil {
		lineage.Source = &PublicSource{
			PublicationID: sourcePubID, Title: sourceTitle,
			SourceVersion: parentVer, InheritedFields: fields,
		}
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}
	rows, err := s.db.Query(ctx, `
SELECT child.id, child.title, child.source_version
  FROM publication_derivations pd
  JOIN publications child ON child.object_id = pd.child_object_id AND child.status = 'active'
 WHERE pd.publication_id = $1
 ORDER BY child.created_at DESC`, publicationID.String())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var branch PublicBranch
		if err := rows.Scan(&branch.PublicationID, &branch.Title, &branch.SourceVersion); err != nil {
			return nil, err
		}
		lineage.Derived = append(lineage.Derived, branch)
	}
	return lineage, rows.Err()
}

// PublicDerive creates an independent private child from an active public
// snapshot. The final transaction re-checks status and derive permission.
func (s *Service) PublicDerive(ctx context.Context, actorID, publicationID uuid.UUID,
	title string, inheritanceFields []string, route, idemKey string, leaseUntil time.Time,
	requestID string, ipHash []byte) (*objects.ObjectView, error) {
	if runeLength(strings.TrimSpace(title)) < 1 || runeLength(title) > maxTitleRunes {
		return nil, fmt.Errorf("%w: title", ErrInvalidRequest)
	}
	if len(inheritanceFields) == 0 {
		return nil, fmt.Errorf("%w: inheritance_fields", ErrInvalidRequest)
	}
	tx, err := s.db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var (
		sourceObjectID string
		sourceVersion  int
		objectType     string
		template       string
		snapshot       []byte
		deriveAllowed  bool
		status         string
	)
	err = tx.QueryRow(ctx, `
SELECT object_id, source_version, object_type, presentation_template, snapshot,
       derive_allowed, status
  FROM publications WHERE id = $1 FOR SHARE`, publicationID.String()).Scan(
		&sourceObjectID, &sourceVersion, &objectType, &template, &snapshot,
		&deriveAllowed, &status)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if status != "active" || !deriveAllowed {
		return nil, ErrNotFound
	}
	available := derive.AvailableFields(objectType)
	if available == nil {
		return nil, fmt.Errorf("%w: object_type", ErrInvalidRequest)
	}
	for _, field := range inheritanceFields {
		if !contains(available, field) {
			return nil, fmt.Errorf("%w: inheritance_fields", ErrInvalidRequest)
		}
	}
	if objectType == objects.TypeInteractive && !contains(inheritanceFields, "options") {
		return nil, fmt.Errorf("%w: inheritance_fields.options", ErrInvalidRequest)
	}
	var snapshotEnvelope struct {
		State json.RawMessage `json:"state"`
	}
	if err := json.Unmarshal(snapshot, &snapshotEnvelope); err != nil {
		return nil, fmt.Errorf("%w: snapshot", ErrInvalidRequest)
	}
	if len(snapshotEnvelope.State) == 0 {
		return nil, fmt.Errorf("%w: snapshot.state", ErrInvalidRequest)
	}
	childState, err := derive.AssembleFromState(objectType, title, snapshotEnvelope.State, inheritanceFields)
	if err != nil {
		return nil, err
	}
	childID, err := uuid.NewV7()
	if err != nil {
		return nil, err
	}
	parentUUID := uuid.MustParse(sourceObjectID)
	view, err := s.objects.CreateDerived(ctx, tx, objects.DerivedInput{
		ChildID:         childID,
		ParentID:        parentUUID,
		ParentType:      objectType,
		Template:        template,
		ActorID:         actorID,
		State:           childState,
		SourceVersion:   sourceVersion,
		InheritedFields: inheritanceFields,
		Lineage: &objects.DeriveLineage{
			Parent:          objects.DeriveParent{ObjectID: publicationID.String(), Version: sourceVersion},
			InheritedFields: append([]string(nil), inheritanceFields...),
			PublicationID:   stringPtr(publicationID.String()),
			Public:          true,
		},
		Route: route, IdemKey: idemKey, LeaseUntil: leaseUntil,
		RequestID: requestID, IPHash: ipHash,
	})
	if err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO publication_derivations (child_object_id, publication_id)
VALUES ($1,$2)`, childID.String(), publicationID.String()); err != nil {
		return nil, mapPublicationError(err)
	}
	if err := s.writeAudit(ctx, tx, actorID, audit.ActionPublicationDerive,
		"living_object", &childID, requestID, ipHash,
		map[string]any{"publication_id": publicationID.String()}); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return view, nil
}

// PublicLineageForChild returns an active public source relation for a child
// created by public derive. The private parent object id is never exposed;
// Parent.ObjectID carries the publication id instead.
func (s *Service) PublicLineageForChild(ctx context.Context, childID uuid.UUID) (*objects.DeriveLineage, error) {
	var (
		publicationID string
		title         string
		sourceVersion int
		fields        []string
	)
	err := s.db.QueryRow(ctx, `
SELECT p.id, p.title, p.source_version, d.inherited_fields
  FROM publication_derivations pd
  JOIN publications p ON p.id = pd.publication_id AND p.status = 'active'
  JOIN derivation_edges d ON d.child_object_id = pd.child_object_id
 WHERE pd.child_object_id = $1`, childID.String()).Scan(
		&publicationID, &title, &sourceVersion, &fields)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	publicationCopy := publicationID
	return &objects.DeriveLineage{
		Parent:           objects.DeriveParent{ObjectID: publicationID, Version: sourceVersion},
		InheritedFields:  fields,
		PublicationID:    &publicationCopy,
		PublicationTitle: &title,
		Public:           true,
	}, nil
}

func buildSnapshot(objectType, template string, state json.RawMessage) (json.RawMessage, string, string, error) {
	var decoded map[string]any
	if err := json.Unmarshal(state, &decoded); err != nil {
		return nil, "", "", fmt.Errorf("%w: state", ErrInvalidRequest)
	}
	title, _ := decoded["title"].(string)
	if runeLength(strings.TrimSpace(title)) < 1 {
		return nil, "", "", fmt.Errorf("%w: state.title", ErrInvalidRequest)
	}
	summary := ""
	switch objectType {
	case objects.TypeCollaborative:
		summary, _ = decoded["summary"].(string)
	case objects.TypeInteractive:
		summary, _ = decoded["prompt"].(string)
	case objects.TypeLivingScene:
		summary, _ = decoded["premise"].(string)
	default:
		return nil, "", "", fmt.Errorf("%w: object_type", ErrInvalidRequest)
	}
	summary = truncateRunes(summary, 2000)
	snapshot := map[string]any{
		"title":                 title,
		"object_type":           objectType,
		"presentation_template": template,
		"state":                 json.RawMessage(state),
	}
	encoded, err := store.MarshalCompactJSON(snapshot)
	if err != nil {
		return nil, "", "", err
	}
	size, err := store.CompactJSONSize(snapshot)
	if err != nil {
		return nil, "", "", err
	}
	if size > 64*1024 {
		return nil, "", "", fmt.Errorf("%w: snapshot", ErrInvalidRequest)
	}
	return encoded, title, summary, nil
}

func contributorIDs(ctx context.Context, tx pgx.Tx, objectID uuid.UUID, sourceVersion int, publisherID uuid.UUID) ([]uuid.UUID, error) {
	rows, err := tx.Query(ctx, `
SELECT DISTINCT actor_user_id
  FROM object_events
 WHERE object_id = $1
   AND object_version <= $2
   AND actor_type = 'user'
   AND actor_user_id IS NOT NULL
   AND actor_user_id <> $3
   AND event_type IN ('object.created','object.state_changed','object.derived')`,
		objectID.String(), sourceVersion, publisherID.String())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := make([]uuid.UUID, 0)
	for rows.Next() {
		var value string
		if err := rows.Scan(&value); err != nil {
			return nil, err
		}
		parsed, err := uuid.Parse(value)
		if err != nil {
			return nil, err
		}
		ids = append(ids, parsed)
	}
	return ids, rows.Err()
}

func mapPublicationError(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		if pgErr.Code == "23505" {
			return ErrConflict
		}
		if pgErr.Code == "23514" {
			return fmt.Errorf("%w: %s", ErrInvalidRequest, pgErr.ConstraintName)
		}
	}
	return err
}

func (s *Service) writeAudit(ctx context.Context, tx pgx.Tx, actorID uuid.UUID, action audit.Action,
	resourceType string, resourceID *uuid.UUID, requestID string, ipHash []byte, metadata map[string]any) error {
	if s.audit == nil {
		return nil
	}
	if metadata == nil {
		metadata = map[string]any{}
	}
	return s.audit.Write(ctx, tx, audit.Event{
		Actor:        audit.UserActor(actorID),
		Action:       action,
		ResourceType: resourceType,
		ResourceID:   resourceID,
		Decision:     "allow",
		RequestID:    requestID,
		IPHash:       ipHash,
		Metadata:     metadata,
	})
}

func runeLength(value string) int {
	return utf8.RuneCountInString(value)
}

func truncateRunes(value string, max int) string {
	runes := []rune(value)
	if len(runes) <= max {
		return value
	}
	return string(runes[:max])
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func stringPtr(value string) *string { return &value }

func timePtr(value time.Time) *time.Time { return &value }
