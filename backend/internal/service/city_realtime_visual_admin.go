package service

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

const (
	cityRealtimeVisualDefaultListLimit        = 50
	cityRealtimeVisualMaximumListLimit        = 100
	cityRealtimeVisualMaximumManifestBytes    = 32 << 10
	cityRealtimeVisualMaximumGenerationTags   = 16
	cityRealtimeVisualGenerationTemplateID    = "city-pixel-asset-v1"
	cityRealtimeVisualGenerationMinimumPixels = 8
	cityRealtimeVisualGenerationMaximumPixels = 1024
	cityRealtimeVisualGenerationMaximumFrames = 64
)

var (
	ErrCityVisualPackNotFound = infraerrors.NotFound(
		"CITY_VISUAL_PACK_NOT_FOUND", "city visual pack was not found",
	)
	ErrCityVisualPackNotStaging = infraerrors.Conflict(
		"CITY_VISUAL_PACK_NOT_STAGING", "city visual pack can only be changed while staging",
	)
	ErrCityVisualPackPublicationBlocked = infraerrors.Conflict(
		"CITY_VISUAL_PACK_PUBLICATION_BLOCKED", "city visual pack cannot be published until review work is resolved",
	)
	ErrCityVisualGenerationJobNotFound = infraerrors.NotFound(
		"CITY_VISUAL_GENERATION_JOB_NOT_FOUND", "city visual generation job was not found",
	)
	ErrCityVisualGenerationState = infraerrors.Conflict(
		"CITY_VISUAL_GENERATION_STATE_INVALID", "city visual generation job is not in a reviewable state",
	)
	ErrCityVisualPackInReleasePolicy = infraerrors.Conflict(
		"CITY_VISUAL_PACK_RELEASED", "city visual pack is still selected by a release policy",
	)
)

// CityRealtimeVisualPackSummary is the bounded, administrator-only release
// inventory. It deliberately excludes raw generation prompts, source URLs and
// storage data; those are never accepted by this control plane in the first
// place.
type CityRealtimeVisualPackSummary struct {
	PackID                    string          `json:"pack_id"`
	PackVersion               string          `json:"pack_version"`
	Status                    string          `json:"status"`
	SemanticProjectionVersion string          `json:"semantic_projection_version"`
	RenderContractVersion     string          `json:"render_contract_version"`
	ManifestHash              string          `json:"manifest_hash"`
	AssetSetHash              string          `json:"asset_set_hash"`
	Compatibility             json.RawMessage `json:"compatibility"`
	CreatedAt                 time.Time       `json:"created_at"`
	PublishedAt               *time.Time      `json:"published_at,omitempty"`
}

type CityRealtimeVisualPackDetail struct {
	CityRealtimeVisualPackSummary
	Manifest   json.RawMessage `json:"manifest"`
	Provenance json.RawMessage `json:"provenance"`
}

type CityRealtimeVisualGenerationJob struct {
	ID               int64           `json:"id"`
	PackID           string          `json:"pack_id"`
	PackVersion      string          `json:"pack_version"`
	AssetClass       string          `json:"asset_class"`
	Status           string          `json:"status"`
	RequestSpec      json.RawMessage `json:"request_spec"`
	CandidateHash    *string         `json:"candidate_hash,omitempty"`
	Review           json.RawMessage `json:"review"`
	CreatedByUserID  *int64          `json:"created_by_user_id,omitempty"`
	ReviewedByUserID *int64          `json:"reviewed_by_user_id,omitempty"`
	CreatedAt        time.Time       `json:"created_at"`
	ReviewedAt       *time.Time      `json:"reviewed_at,omitempty"`
}

type CityRealtimeVisualReleasePolicy struct {
	SemanticProjectionVersion string    `json:"semantic_projection_version"`
	SpatialProfileID          string    `json:"spatial_profile_id"`
	PackID                    string    `json:"pack_id"`
	PackVersion               string    `json:"pack_version"`
	CreatedAt                 time.Time `json:"created_at"`
	UpdatedAt                 time.Time `json:"updated_at"`
	CreatedByUserID           *int64    `json:"created_by_user_id,omitempty"`
	UpdatedByUserID           *int64    `json:"updated_by_user_id,omitempty"`
}

type CityRealtimeVisualReviewEvent struct {
	ID              int64           `json:"id"`
	PackID          string          `json:"pack_id"`
	PackVersion     string          `json:"pack_version"`
	GenerationJobID *int64          `json:"generation_job_id,omitempty"`
	EventType       string          `json:"event_type"`
	ActorUserID     *int64          `json:"actor_user_id,omitempty"`
	Metadata        json.RawMessage `json:"metadata"`
	CreatedAt       time.Time       `json:"created_at"`
}

type CityRealtimeVisualPackCreateInput struct {
	ActorUserID       int64
	PackID            string
	PackVersion       string
	SpatialProfileIDs []string
	Manifest          json.RawMessage
}

type CityRealtimeVisualPackUpdateInput struct {
	ActorUserID       int64
	PackID            string
	PackVersion       string
	SpatialProfileIDs []string
	Manifest          json.RawMessage
}

type CityRealtimeVisualGenerationJobCreateInput struct {
	ActorUserID  int64
	PackID       string
	PackVersion  string
	AssetClass   string
	SemanticTags []string
	PixelWidth   int
	PixelHeight  int
	FrameCount   int
}

type CityRealtimeVisualGenerationJobReviewInput struct {
	ActorUserID int64
	PackID      string
	PackVersion string
	JobID       int64
	Decision    string
	ReasonCode  string
}

type CityRealtimeVisualReleasePolicySetInput struct {
	ActorUserID      int64
	SpatialProfileID string
	PackID           string
	PackVersion      string
}

type normalizedCityRealtimeVisualPackInput struct {
	packID        string
	packVersion   string
	compatibility json.RawMessage
	manifest      json.RawMessage
}

type normalizedCityRealtimeVisualGenerationJobInput struct {
	packID      string
	packVersion string
	assetClass  string
	requestSpec json.RawMessage
}

type normalizedCityRealtimeVisualGenerationReviewInput struct {
	packID      string
	packVersion string
	jobID       int64
	decision    string
	reasonCode  string
}

// ListRealtimeVisualPacks returns the bounded control-plane inventory. This
// endpoint is administrator scoped in both the route middleware and the
// service so direct callers cannot bypass the boundary.
func (s *CityEconomyService) ListRealtimeVisualPacks(
	ctx context.Context,
	limit int,
) ([]*CityRealtimeVisualPackSummary, error) {
	if err := s.requireCityRealtimeVisualAdministration(ctx); err != nil {
		return nil, err
	}
	limit, err := normalizeCityRealtimeVisualListLimit(limit)
	if err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT pack_id, pack_version, status, semantic_projection_version,
       render_contract_version, manifest_hash, asset_set_hash, compatibility,
       created_at, published_at
FROM city_visual_packs
ORDER BY created_at DESC, pack_id ASC, pack_version DESC
LIMIT $1`, limit)
	if err != nil {
		return nil, fmt.Errorf("list city realtime visual packs: %w", err)
	}
	defer func() { _ = rows.Close() }()
	items := make([]*CityRealtimeVisualPackSummary, 0, limit)
	for rows.Next() {
		item, scanErr := scanCityRealtimeVisualPackSummary(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		items = append(items, item)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate city realtime visual packs: %w", err)
	}
	return items, nil
}

func (s *CityEconomyService) GetRealtimeVisualPack(
	ctx context.Context,
	packID, packVersion string,
) (*CityRealtimeVisualPackDetail, error) {
	if err := s.requireCityRealtimeVisualAdministration(ctx); err != nil {
		return nil, err
	}
	if err := validateCityRealtimeVisualPackIdentity(packID, packVersion); err != nil {
		return nil, err
	}
	return loadCityRealtimeVisualPackDetail(ctx, s.db, strings.TrimSpace(packID), strings.TrimSpace(packVersion), false)
}

// CreateRealtimeVisualPack creates a server-owned staging pack. It is limited
// to the currently deployable procedural contract; atlas images require the
// later isolated asset-ingestion pipeline and cannot be smuggled in through a
// manifest, URL, base64 payload or raw provider prompt.
func (s *CityEconomyService) CreateRealtimeVisualPack(
	ctx context.Context,
	input CityRealtimeVisualPackCreateInput,
) (*CityRealtimeVisualPackDetail, error) {
	if err := s.requireCityRealtimeVisualAdministration(ctx); err != nil {
		return nil, err
	}
	if input.ActorUserID <= 0 {
		return nil, ErrCityInvalidInput.WithMetadata(map[string]string{"field": "actor_user_id"})
	}
	normalized, err := normalizeCityRealtimeVisualPackInput(
		input.PackID, input.PackVersion, input.SpatialProfileIDs, input.Manifest,
	)
	if err != nil {
		return nil, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin city visual pack staging transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	item, err := insertCityRealtimeVisualPackStaging(ctx, tx, normalized)
	if err != nil {
		return nil, err
	}
	if err = recordCityRealtimeVisualReviewEvent(ctx, tx, item.PackID, item.PackVersion, nil,
		"staging_created", input.ActorUserID, map[string]string{
			"render_contract_version":     item.RenderContractVersion,
			"semantic_projection_version": item.SemanticProjectionVersion,
		}); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit city visual pack staging transaction: %w", err)
	}
	return item, nil
}

func (s *CityEconomyService) UpdateRealtimeVisualPack(
	ctx context.Context,
	input CityRealtimeVisualPackUpdateInput,
) (*CityRealtimeVisualPackDetail, error) {
	if err := s.requireCityRealtimeVisualAdministration(ctx); err != nil {
		return nil, err
	}
	if input.ActorUserID <= 0 {
		return nil, ErrCityInvalidInput.WithMetadata(map[string]string{"field": "actor_user_id"})
	}
	normalized, err := normalizeCityRealtimeVisualPackInput(
		input.PackID, input.PackVersion, input.SpatialProfileIDs, input.Manifest,
	)
	if err != nil {
		return nil, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin city visual pack update transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	current, err := loadCityRealtimeVisualPackDetail(ctx, tx, normalized.packID, normalized.packVersion, true)
	if err != nil {
		return nil, err
	}
	if current.Status != "staging" {
		return nil, ErrCityVisualPackNotStaging
	}
	item, err := updateCityRealtimeVisualPackStaging(ctx, tx, normalized)
	if err != nil {
		return nil, err
	}
	if err = recordCityRealtimeVisualReviewEvent(ctx, tx, item.PackID, item.PackVersion, nil,
		"manifest_updated", input.ActorUserID, map[string]string{
			"manifest_hash":  item.ManifestHash,
			"asset_set_hash": item.AssetSetHash,
		}); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit city visual pack update transaction: %w", err)
	}
	return item, nil
}

// CreateRealtimeVisualGenerationJob records a provider-agnostic, structured
// request only. It contains finite semantic tags and dimensions; raw prompts,
// model names, source images and remote URLs remain server-side worker concerns
// and have no browser-facing control plane field.
func (s *CityEconomyService) CreateRealtimeVisualGenerationJob(
	ctx context.Context,
	input CityRealtimeVisualGenerationJobCreateInput,
) (*CityRealtimeVisualGenerationJob, error) {
	if err := s.requireCityRealtimeVisualAdministration(ctx); err != nil {
		return nil, err
	}
	if input.ActorUserID <= 0 {
		return nil, ErrCityInvalidInput.WithMetadata(map[string]string{"field": "actor_user_id"})
	}
	normalized, err := normalizeCityRealtimeVisualGenerationJobInput(input)
	if err != nil {
		return nil, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin city visual generation job transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	pack, err := loadCityRealtimeVisualPackDetail(ctx, tx, normalized.packID, normalized.packVersion, true)
	if err != nil {
		return nil, err
	}
	if pack.Status != "staging" {
		return nil, ErrCityVisualPackNotStaging
	}
	job := &CityRealtimeVisualGenerationJob{PackID: normalized.packID, PackVersion: normalized.packVersion}
	var requestSpec, review []byte
	var candidateHash sql.NullString
	var createdBy, reviewedBy sql.NullInt64
	var reviewedAt sql.NullTime
	err = tx.QueryRowContext(ctx, `
INSERT INTO city_visual_generation_jobs
    (pack_id, pack_version, asset_class, status, request_spec, created_by_user_id)
VALUES ($1, $2, $3, 'queued', $4::jsonb, $5)
RETURNING id, asset_class, status, request_spec, candidate_hash, review,
          created_by_user_id, reviewed_by_user_id, created_at, reviewed_at`,
		normalized.packID, normalized.packVersion, normalized.assetClass,
		string(normalized.requestSpec), input.ActorUserID,
	).Scan(
		&job.ID, &job.AssetClass, &job.Status, &requestSpec, &candidateHash, &review,
		&createdBy, &reviewedBy, &job.CreatedAt, &reviewedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("create city visual generation job: %w", err)
	}
	job.RequestSpec = cloneCityRealtimeVisualJSON(requestSpec)
	job.Review = cloneCityRealtimeVisualJSON(review)
	job.CandidateHash = cityRealtimeVisualNullStringPointer(candidateHash)
	job.CreatedByUserID = cityRealtimeVisualNullInt64Pointer(createdBy)
	job.ReviewedByUserID = cityRealtimeVisualNullInt64Pointer(reviewedBy)
	job.ReviewedAt = cityRealtimeVisualNullTimePointer(reviewedAt)
	job.CreatedAt = job.CreatedAt.UTC().Truncate(time.Microsecond)
	jobID := job.ID
	if err = recordCityRealtimeVisualReviewEvent(ctx, tx, normalized.packID, normalized.packVersion, &jobID,
		"generation_requested", input.ActorUserID, map[string]string{
			"asset_class": normalized.assetClass,
			"job_status":  job.Status,
		}); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit city visual generation job transaction: %w", err)
	}
	return job, nil
}

// ReviewRealtimeVisualGenerationJob can approve/reject a worker-produced
// candidate or cancel an in-flight request. Approval alone still cannot make
// pixels visible: publication remains blocked until a future secure asset
// materialisation worker writes a reviewed asset into the staging pack.
func (s *CityEconomyService) ReviewRealtimeVisualGenerationJob(
	ctx context.Context,
	input CityRealtimeVisualGenerationJobReviewInput,
) (*CityRealtimeVisualGenerationJob, error) {
	if err := s.requireCityRealtimeVisualAdministration(ctx); err != nil {
		return nil, err
	}
	if input.ActorUserID <= 0 {
		return nil, ErrCityInvalidInput.WithMetadata(map[string]string{"field": "actor_user_id"})
	}
	normalized, err := normalizeCityRealtimeVisualGenerationReviewInput(input)
	if err != nil {
		return nil, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin city visual generation review transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	pack, err := loadCityRealtimeVisualPackDetail(ctx, tx, normalized.packID, normalized.packVersion, true)
	if err != nil {
		return nil, err
	}
	if pack.Status != "staging" {
		return nil, ErrCityVisualPackNotStaging
	}
	job, err := loadCityRealtimeVisualGenerationJobForUpdate(ctx, tx, normalized.packID, normalized.packVersion, normalized.jobID)
	if err != nil {
		return nil, err
	}
	if !cityRealtimeVisualGenerationReviewAllowed(job.Status, normalized.decision) {
		return nil, ErrCityVisualGenerationState
	}
	review, err := json.Marshal(map[string]string{
		"decision":    normalized.decision,
		"reason_code": normalized.reasonCode,
	})
	if err != nil {
		return nil, fmt.Errorf("encode city visual generation review: %w", err)
	}
	var requestSpec, storedReview []byte
	var candidateHash sql.NullString
	var createdBy, reviewedBy sql.NullInt64
	var reviewedAt sql.NullTime
	err = tx.QueryRowContext(ctx, `
UPDATE city_visual_generation_jobs
SET status = $1,
    review = $2::jsonb,
    reviewed_by_user_id = $3,
    reviewed_at = NOW()
WHERE id = $4
  AND pack_id = $5
  AND pack_version = $6
RETURNING id, asset_class, status, request_spec, candidate_hash, review,
          created_by_user_id, reviewed_by_user_id, created_at, reviewed_at`,
		normalized.decision, string(review), input.ActorUserID,
		normalized.jobID, normalized.packID, normalized.packVersion,
	).Scan(
		&job.ID, &job.AssetClass, &job.Status, &requestSpec, &candidateHash, &storedReview,
		&createdBy, &reviewedBy, &job.CreatedAt, &reviewedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("update city visual generation review: %w", err)
	}
	job.RequestSpec = cloneCityRealtimeVisualJSON(requestSpec)
	job.Review = cloneCityRealtimeVisualJSON(storedReview)
	job.CandidateHash = cityRealtimeVisualNullStringPointer(candidateHash)
	job.CreatedByUserID = cityRealtimeVisualNullInt64Pointer(createdBy)
	job.ReviewedByUserID = cityRealtimeVisualNullInt64Pointer(reviewedBy)
	job.ReviewedAt = cityRealtimeVisualNullTimePointer(reviewedAt)
	job.CreatedAt = job.CreatedAt.UTC().Truncate(time.Microsecond)
	jobID := job.ID
	if err = recordCityRealtimeVisualReviewEvent(ctx, tx, normalized.packID, normalized.packVersion, &jobID,
		"generation_reviewed", input.ActorUserID, map[string]string{
			"decision":    normalized.decision,
			"reason_code": normalized.reasonCode,
		}); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit city visual generation review transaction: %w", err)
	}
	return job, nil
}

func (s *CityEconomyService) ListRealtimeVisualGenerationJobs(
	ctx context.Context,
	packID, packVersion string,
	limit int,
) ([]*CityRealtimeVisualGenerationJob, error) {
	if err := s.requireCityRealtimeVisualAdministration(ctx); err != nil {
		return nil, err
	}
	if err := validateCityRealtimeVisualPackIdentity(packID, packVersion); err != nil {
		return nil, err
	}
	limit, err := normalizeCityRealtimeVisualListLimit(limit)
	if err != nil {
		return nil, err
	}
	packID = strings.TrimSpace(packID)
	packVersion = strings.TrimSpace(packVersion)
	if _, err = loadCityRealtimeVisualPackDetail(ctx, s.db, packID, packVersion, false); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT id, asset_class, status, request_spec, candidate_hash, review,
       created_by_user_id, reviewed_by_user_id, created_at, reviewed_at
FROM city_visual_generation_jobs
WHERE pack_id = $1 AND pack_version = $2
ORDER BY id DESC
LIMIT $3`, packID, packVersion, limit)
	if err != nil {
		return nil, fmt.Errorf("list city visual generation jobs: %w", err)
	}
	defer func() { _ = rows.Close() }()
	items := make([]*CityRealtimeVisualGenerationJob, 0, limit)
	for rows.Next() {
		item, scanErr := scanCityRealtimeVisualGenerationJob(rows, packID, packVersion)
		if scanErr != nil {
			return nil, scanErr
		}
		items = append(items, item)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate city visual generation jobs: %w", err)
	}
	return items, nil
}

func (s *CityEconomyService) PublishRealtimeVisualPack(
	ctx context.Context,
	actorUserID int64,
	packID, packVersion string,
) (*CityRealtimeVisualPackDetail, error) {
	if err := s.requireCityRealtimeVisualAdministration(ctx); err != nil {
		return nil, err
	}
	if actorUserID <= 0 {
		return nil, ErrCityInvalidInput.WithMetadata(map[string]string{"field": "actor_user_id"})
	}
	if err := validateCityRealtimeVisualPackIdentity(packID, packVersion); err != nil {
		return nil, err
	}
	packID = strings.TrimSpace(packID)
	packVersion = strings.TrimSpace(packVersion)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin city visual pack publication transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	current, err := loadCityRealtimeVisualPackDetail(ctx, tx, packID, packVersion, true)
	if err != nil {
		return nil, err
	}
	if current.Status != "staging" {
		return nil, ErrCityVisualPackNotStaging
	}
	if current.SemanticProjectionVersion != cityRealtimeSemanticProjectionVersion ||
		current.RenderContractVersion != cityRealtimeProceduralPixelRenderContract {
		return nil, ErrCityInvalidInput.WithMetadata(map[string]string{"field": "render_contract_version"})
	}
	var unresolvedJobs int
	if err = tx.QueryRowContext(ctx, `
SELECT COUNT(*)
FROM city_visual_generation_jobs
WHERE pack_id = $1
  AND pack_version = $2
  AND status NOT IN ('rejected', 'cancelled', 'failed')`, packID, packVersion).Scan(&unresolvedJobs); err != nil {
		return nil, fmt.Errorf("count unresolved city visual generation jobs: %w", err)
	}
	if unresolvedJobs > 0 {
		return nil, ErrCityVisualPackPublicationBlocked.WithMetadata(map[string]string{"field": "generation_jobs"})
	}
	item, err := publishCityRealtimeVisualPack(ctx, tx, packID, packVersion)
	if err != nil {
		return nil, err
	}
	if err = recordCityRealtimeVisualReviewEvent(ctx, tx, packID, packVersion, nil,
		"published", actorUserID, map[string]string{
			"manifest_hash":  item.ManifestHash,
			"asset_set_hash": item.AssetSetHash,
		}); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit city visual pack publication transaction: %w", err)
	}
	return item, nil
}

// RetireRealtimeVisualPack prevents a pack from being selected for new worlds
// without breaking worlds already bound to it. A policy must be moved first,
// which makes the operator's rollout deliberate and reversible by publishing a
// new pack rather than mutating old shared worlds.
func (s *CityEconomyService) RetireRealtimeVisualPack(
	ctx context.Context,
	actorUserID int64,
	packID, packVersion string,
) (*CityRealtimeVisualPackDetail, error) {
	if err := s.requireCityRealtimeVisualAdministration(ctx); err != nil {
		return nil, err
	}
	if actorUserID <= 0 {
		return nil, ErrCityInvalidInput.WithMetadata(map[string]string{"field": "actor_user_id"})
	}
	if err := validateCityRealtimeVisualPackIdentity(packID, packVersion); err != nil {
		return nil, err
	}
	packID = strings.TrimSpace(packID)
	packVersion = strings.TrimSpace(packVersion)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin city visual pack retirement transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	current, err := loadCityRealtimeVisualPackDetail(ctx, tx, packID, packVersion, true)
	if err != nil {
		return nil, err
	}
	if current.Status != "published" {
		return nil, ErrCityVisualPackNotStaging
	}
	var selectedByPolicy bool
	if err = tx.QueryRowContext(ctx, `
SELECT EXISTS(
    SELECT 1
    FROM city_visual_pack_release_policies
    WHERE pack_id = $1 AND pack_version = $2
)`, packID, packVersion).Scan(&selectedByPolicy); err != nil {
		return nil, fmt.Errorf("check city visual release policy references: %w", err)
	}
	if selectedByPolicy {
		return nil, ErrCityVisualPackInReleasePolicy
	}
	item, err := retireCityRealtimeVisualPack(ctx, tx, packID, packVersion)
	if err != nil {
		return nil, err
	}
	if err = recordCityRealtimeVisualReviewEvent(ctx, tx, packID, packVersion, nil,
		"retired", actorUserID, map[string]string{"previous_status": "published"}); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit city visual pack retirement transaction: %w", err)
	}
	return item, nil
}

func (s *CityEconomyService) ListRealtimeVisualReleasePolicies(
	ctx context.Context,
) ([]*CityRealtimeVisualReleasePolicy, error) {
	if err := s.requireCityRealtimeVisualAdministration(ctx); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT semantic_projection_version, spatial_profile_id, pack_id, pack_version,
       created_at, updated_at, created_by_user_id, updated_by_user_id
FROM city_visual_pack_release_policies
ORDER BY semantic_projection_version ASC,
         CASE WHEN spatial_profile_id = '*' THEN 1 ELSE 0 END ASC,
         spatial_profile_id ASC`)
	if err != nil {
		return nil, fmt.Errorf("list city visual release policies: %w", err)
	}
	defer func() { _ = rows.Close() }()
	items := make([]*CityRealtimeVisualReleasePolicy, 0)
	for rows.Next() {
		item := &CityRealtimeVisualReleasePolicy{}
		var createdBy, updatedBy sql.NullInt64
		if err = rows.Scan(
			&item.SemanticProjectionVersion, &item.SpatialProfileID,
			&item.PackID, &item.PackVersion, &item.CreatedAt, &item.UpdatedAt,
			&createdBy, &updatedBy,
		); err != nil {
			return nil, fmt.Errorf("scan city visual release policy: %w", err)
		}
		item.CreatedAt = item.CreatedAt.UTC().Truncate(time.Microsecond)
		item.UpdatedAt = item.UpdatedAt.UTC().Truncate(time.Microsecond)
		item.CreatedByUserID = cityRealtimeVisualNullInt64Pointer(createdBy)
		item.UpdatedByUserID = cityRealtimeVisualNullInt64Pointer(updatedBy)
		items = append(items, item)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate city visual release policies: %w", err)
	}
	return items, nil
}

func (s *CityEconomyService) SetRealtimeVisualReleasePolicy(
	ctx context.Context,
	input CityRealtimeVisualReleasePolicySetInput,
) (*CityRealtimeVisualReleasePolicy, error) {
	if err := s.requireCityRealtimeVisualAdministration(ctx); err != nil {
		return nil, err
	}
	if input.ActorUserID <= 0 {
		return nil, ErrCityInvalidInput.WithMetadata(map[string]string{"field": "actor_user_id"})
	}
	profileID := strings.TrimSpace(input.SpatialProfileID)
	if profileID != "*" && !cityRealtimeVisualIdentifierValid(profileID, 64) {
		return nil, ErrCityInvalidInput.WithMetadata(map[string]string{"field": "spatial_profile_id"})
	}
	if err := validateCityRealtimeVisualPackIdentity(input.PackID, input.PackVersion); err != nil {
		return nil, err
	}
	packID := strings.TrimSpace(input.PackID)
	packVersion := strings.TrimSpace(input.PackVersion)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin city visual release policy transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	pack, err := loadCityRealtimeVisualPackDetail(ctx, tx, packID, packVersion, true)
	if err != nil {
		return nil, err
	}
	if pack.Status != "published" ||
		pack.SemanticProjectionVersion != cityRealtimeSemanticProjectionVersion ||
		pack.RenderContractVersion != cityRealtimeProceduralPixelRenderContract ||
		!cityRealtimeVisualPackSupportsReleasePolicy(pack.Compatibility, profileID) {
		return nil, ErrCityInvalidInput.WithMetadata(map[string]string{"field": "release_policy"})
	}
	item := &CityRealtimeVisualReleasePolicy{}
	var createdBy, updatedBy sql.NullInt64
	err = tx.QueryRowContext(ctx, `
INSERT INTO city_visual_pack_release_policies
    (semantic_projection_version, spatial_profile_id, pack_id, pack_version,
     created_by_user_id, updated_by_user_id)
VALUES ($1, $2, $3, $4, $5, $5)
ON CONFLICT (semantic_projection_version, spatial_profile_id) DO UPDATE
SET pack_id = EXCLUDED.pack_id,
    pack_version = EXCLUDED.pack_version,
    updated_by_user_id = EXCLUDED.updated_by_user_id,
    updated_at = NOW()
RETURNING semantic_projection_version, spatial_profile_id, pack_id, pack_version,
          created_at, updated_at, created_by_user_id, updated_by_user_id`,
		cityRealtimeSemanticProjectionVersion, profileID, packID, packVersion, input.ActorUserID,
	).Scan(
		&item.SemanticProjectionVersion, &item.SpatialProfileID,
		&item.PackID, &item.PackVersion, &item.CreatedAt, &item.UpdatedAt,
		&createdBy, &updatedBy,
	)
	if err != nil {
		return nil, fmt.Errorf("assign city visual release policy: %w", err)
	}
	item.CreatedAt = item.CreatedAt.UTC().Truncate(time.Microsecond)
	item.UpdatedAt = item.UpdatedAt.UTC().Truncate(time.Microsecond)
	item.CreatedByUserID = cityRealtimeVisualNullInt64Pointer(createdBy)
	item.UpdatedByUserID = cityRealtimeVisualNullInt64Pointer(updatedBy)
	if err = recordCityRealtimeVisualReviewEvent(ctx, tx, packID, packVersion, nil,
		"release_policy_assigned", input.ActorUserID, map[string]string{
			"spatial_profile_id":          profileID,
			"semantic_projection_version": cityRealtimeSemanticProjectionVersion,
		}); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit city visual release policy transaction: %w", err)
	}
	return item, nil
}

func (s *CityEconomyService) ListRealtimeVisualReviewEvents(
	ctx context.Context,
	packID, packVersion string,
	limit int,
) ([]*CityRealtimeVisualReviewEvent, error) {
	if err := s.requireCityRealtimeVisualAdministration(ctx); err != nil {
		return nil, err
	}
	if err := validateCityRealtimeVisualPackIdentity(packID, packVersion); err != nil {
		return nil, err
	}
	limit, err := normalizeCityRealtimeVisualListLimit(limit)
	if err != nil {
		return nil, err
	}
	packID = strings.TrimSpace(packID)
	packVersion = strings.TrimSpace(packVersion)
	if _, err = loadCityRealtimeVisualPackDetail(ctx, s.db, packID, packVersion, false); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT id, generation_job_id, event_type, actor_user_id, metadata, created_at
FROM city_visual_pack_review_events
WHERE pack_id = $1 AND pack_version = $2
ORDER BY id DESC
LIMIT $3`, packID, packVersion, limit)
	if err != nil {
		return nil, fmt.Errorf("list city visual review events: %w", err)
	}
	defer func() { _ = rows.Close() }()
	items := make([]*CityRealtimeVisualReviewEvent, 0, limit)
	for rows.Next() {
		item := &CityRealtimeVisualReviewEvent{PackID: packID, PackVersion: packVersion}
		var jobID, actorID sql.NullInt64
		var metadata []byte
		if err = rows.Scan(&item.ID, &jobID, &item.EventType, &actorID, &metadata, &item.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan city visual review event: %w", err)
		}
		item.GenerationJobID = cityRealtimeVisualNullInt64Pointer(jobID)
		item.ActorUserID = cityRealtimeVisualNullInt64Pointer(actorID)
		item.Metadata = cloneCityRealtimeVisualJSON(metadata)
		item.CreatedAt = item.CreatedAt.UTC().Truncate(time.Microsecond)
		items = append(items, item)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate city visual review events: %w", err)
	}
	return items, nil
}

func (s *CityEconomyService) requireCityRealtimeVisualAdministration(ctx context.Context) error {
	if !IsCitySystemAdministrator(ctx) {
		return ErrCityManagementRequired
	}
	if s == nil || s.db == nil {
		return ErrCityInvalidInput
	}
	return nil
}

func normalizeCityRealtimeVisualListLimit(limit int) (int, error) {
	if limit <= 0 {
		return cityRealtimeVisualDefaultListLimit, nil
	}
	if limit > cityRealtimeVisualMaximumListLimit {
		return 0, ErrCityInvalidInput.WithMetadata(map[string]string{"field": "limit"})
	}
	return limit, nil
}

func validateCityRealtimeVisualPackIdentity(packID, packVersion string) error {
	if !cityRealtimeVisualIdentifierValid(strings.TrimSpace(packID), 96) ||
		!cityRealtimeVisualVersionValid(strings.TrimSpace(packVersion)) {
		return ErrCityInvalidInput.WithMetadata(map[string]string{"field": "visual_pack"})
	}
	return nil
}

func normalizeCityRealtimeVisualPackInput(
	packID, packVersion string,
	profileIDs []string,
	manifest json.RawMessage,
) (normalizedCityRealtimeVisualPackInput, error) {
	if err := validateCityRealtimeVisualPackIdentity(packID, packVersion); err != nil {
		return normalizedCityRealtimeVisualPackInput{}, err
	}
	normalizedProfiles, err := normalizeCityRealtimeVisualProfileIDs(profileIDs)
	if err != nil {
		return normalizedCityRealtimeVisualPackInput{}, err
	}
	normalizedManifest, err := normalizeCityRealtimeProceduralManifest(manifest)
	if err != nil {
		return normalizedCityRealtimeVisualPackInput{}, err
	}
	compatibility, err := json.Marshal(struct {
		SpatialProfileIDs          []string `json:"spatial_profile_ids"`
		SemanticProjectionVersions []string `json:"semantic_projection_versions"`
	}{
		SpatialProfileIDs:          normalizedProfiles,
		SemanticProjectionVersions: []string{cityRealtimeSemanticProjectionVersion},
	})
	if err != nil {
		return normalizedCityRealtimeVisualPackInput{}, fmt.Errorf("encode city visual compatibility: %w", err)
	}
	return normalizedCityRealtimeVisualPackInput{
		packID: strings.TrimSpace(packID), packVersion: strings.TrimSpace(packVersion),
		compatibility: json.RawMessage(compatibility), manifest: normalizedManifest,
	}, nil
}

func normalizeCityRealtimeVisualProfileIDs(values []string) ([]string, error) {
	if len(values) == 0 || len(values) > cityRealtimeVisualMaximumGenerationTags {
		return nil, ErrCityInvalidInput.WithMetadata(map[string]string{"field": "spatial_profile_ids"})
	}
	seen := make(map[string]struct{}, len(values))
	normalized := make([]string, 0, len(values))
	for _, raw := range values {
		value := strings.TrimSpace(raw)
		if value != "*" && !cityRealtimeVisualIdentifierValid(value, 64) {
			return nil, ErrCityInvalidInput.WithMetadata(map[string]string{"field": "spatial_profile_ids"})
		}
		if _, exists := seen[value]; exists {
			return nil, ErrCityInvalidInput.WithMetadata(map[string]string{"field": "spatial_profile_ids"})
		}
		seen[value] = struct{}{}
		normalized = append(normalized, value)
	}
	if _, wildcard := seen["*"]; wildcard && len(normalized) != 1 {
		return nil, ErrCityInvalidInput.WithMetadata(map[string]string{"field": "spatial_profile_ids"})
	}
	sort.Strings(normalized)
	return normalized, nil
}

func normalizeCityRealtimeProceduralManifest(raw json.RawMessage) (json.RawMessage, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || len(trimmed) > cityRealtimeVisualMaximumManifestBytes || !json.Valid(trimmed) {
		return nil, ErrCityInvalidInput.WithMetadata(map[string]string{"field": "manifest"})
	}
	if err := validateCityRealtimeVisualManifest(trimmed, cityRealtimeProceduralPixelRenderContract); err != nil {
		return nil, err
	}
	var root map[string]json.RawMessage
	if err := json.Unmarshal(trimmed, &root); err != nil {
		return nil, ErrCityInvalidInput.WithMetadata(map[string]string{"field": "manifest"})
	}
	allowedKeys := map[string]struct{}{
		"schema_version": {}, "render_mode": {}, "logical_tile_px": {},
		"profile_palettes": {}, "semantic_rules": {}, "assets": {},
	}
	for key := range root {
		if _, allowed := allowedKeys[key]; !allowed {
			return nil, ErrCityInvalidInput.WithMetadata(map[string]string{"field": "manifest"})
		}
	}
	var assets []json.RawMessage
	if err := json.Unmarshal(root["assets"], &assets); err != nil || len(assets) != 0 {
		return nil, ErrCityInvalidInput.WithMetadata(map[string]string{"field": "assets"})
	}
	if rules, exists := root["semantic_rules"]; exists {
		var decoded map[string][]string
		if err := json.Unmarshal(rules, &decoded); err != nil || len(decoded) > 8 {
			return nil, ErrCityInvalidInput.WithMetadata(map[string]string{"field": "semantic_rules"})
		}
		for key, values := range decoded {
			if !cityRealtimeVisualIdentifierValid(key, 64) || len(values) > 64 {
				return nil, ErrCityInvalidInput.WithMetadata(map[string]string{"field": "semantic_rules"})
			}
			for _, value := range values {
				if !cityRealtimeVisualIdentifierValid(value, 128) {
					return nil, ErrCityInvalidInput.WithMetadata(map[string]string{"field": "semantic_rules"})
				}
			}
		}
	}
	canonical, err := json.Marshal(root)
	if err != nil {
		return nil, fmt.Errorf("canonicalize city visual manifest: %w", err)
	}
	return json.RawMessage(canonical), nil
}

func normalizeCityRealtimeVisualGenerationJobInput(
	input CityRealtimeVisualGenerationJobCreateInput,
) (normalizedCityRealtimeVisualGenerationJobInput, error) {
	if err := validateCityRealtimeVisualPackIdentity(input.PackID, input.PackVersion); err != nil {
		return normalizedCityRealtimeVisualGenerationJobInput{}, err
	}
	assetClass := strings.TrimSpace(input.AssetClass)
	if !cityRealtimeVisualAssetClassValid(assetClass) ||
		input.PixelWidth < cityRealtimeVisualGenerationMinimumPixels ||
		input.PixelWidth > cityRealtimeVisualGenerationMaximumPixels ||
		input.PixelHeight < cityRealtimeVisualGenerationMinimumPixels ||
		input.PixelHeight > cityRealtimeVisualGenerationMaximumPixels ||
		input.PixelWidth%8 != 0 || input.PixelHeight%8 != 0 ||
		input.FrameCount < 1 || input.FrameCount > cityRealtimeVisualGenerationMaximumFrames {
		return normalizedCityRealtimeVisualGenerationJobInput{}, ErrCityInvalidInput.WithMetadata(map[string]string{"field": "generation_request"})
	}
	if len(input.SemanticTags) == 0 || len(input.SemanticTags) > cityRealtimeVisualMaximumGenerationTags {
		return normalizedCityRealtimeVisualGenerationJobInput{}, ErrCityInvalidInput.WithMetadata(map[string]string{"field": "semantic_tags"})
	}
	seen := make(map[string]struct{}, len(input.SemanticTags))
	tags := make([]string, 0, len(input.SemanticTags))
	for _, raw := range input.SemanticTags {
		tag := strings.TrimSpace(raw)
		if !cityRealtimeVisualIdentifierValid(tag, 128) {
			return normalizedCityRealtimeVisualGenerationJobInput{}, ErrCityInvalidInput.WithMetadata(map[string]string{"field": "semantic_tags"})
		}
		if _, duplicate := seen[tag]; duplicate {
			return normalizedCityRealtimeVisualGenerationJobInput{}, ErrCityInvalidInput.WithMetadata(map[string]string{"field": "semantic_tags"})
		}
		seen[tag] = struct{}{}
		tags = append(tags, tag)
	}
	sort.Strings(tags)
	requestSpec, err := json.Marshal(struct {
		SchemaVersion         int      `json:"schema_version"`
		AssetClass            string   `json:"asset_class"`
		SemanticTags          []string `json:"semantic_tags"`
		PixelWidth            int      `json:"pixel_width"`
		PixelHeight           int      `json:"pixel_height"`
		FrameCount            int      `json:"frame_count"`
		PromptTemplateID      string   `json:"prompt_template_id"`
		RenderContractVersion string   `json:"render_contract_version"`
	}{
		SchemaVersion: 1, AssetClass: assetClass, SemanticTags: tags,
		PixelWidth: input.PixelWidth, PixelHeight: input.PixelHeight, FrameCount: input.FrameCount,
		PromptTemplateID:      cityRealtimeVisualGenerationTemplateID,
		RenderContractVersion: cityRealtimeProceduralPixelRenderContract,
	})
	if err != nil {
		return normalizedCityRealtimeVisualGenerationJobInput{}, fmt.Errorf("encode city visual generation request: %w", err)
	}
	return normalizedCityRealtimeVisualGenerationJobInput{
		packID: strings.TrimSpace(input.PackID), packVersion: strings.TrimSpace(input.PackVersion),
		assetClass: assetClass, requestSpec: json.RawMessage(requestSpec),
	}, nil
}

func normalizeCityRealtimeVisualGenerationReviewInput(
	input CityRealtimeVisualGenerationJobReviewInput,
) (normalizedCityRealtimeVisualGenerationReviewInput, error) {
	if err := validateCityRealtimeVisualPackIdentity(input.PackID, input.PackVersion); err != nil {
		return normalizedCityRealtimeVisualGenerationReviewInput{}, err
	}
	decision := strings.TrimSpace(input.Decision)
	if input.JobID <= 0 || (decision != "approved" && decision != "rejected" && decision != "cancelled") {
		return normalizedCityRealtimeVisualGenerationReviewInput{}, ErrCityInvalidInput.WithMetadata(map[string]string{"field": "generation_review"})
	}
	reasonCode := strings.TrimSpace(input.ReasonCode)
	if reasonCode == "" {
		reasonCode = "operator_decision"
	}
	if !cityRealtimeVisualIdentifierValid(reasonCode, 64) {
		return normalizedCityRealtimeVisualGenerationReviewInput{}, ErrCityInvalidInput.WithMetadata(map[string]string{"field": "reason_code"})
	}
	return normalizedCityRealtimeVisualGenerationReviewInput{
		packID: strings.TrimSpace(input.PackID), packVersion: strings.TrimSpace(input.PackVersion),
		jobID: input.JobID, decision: decision, reasonCode: reasonCode,
	}, nil
}

func cityRealtimeVisualAssetClassValid(value string) bool {
	switch value {
	case "terrain", "infrastructure", "building_exterior", "interior", "furniture", "item",
		"vehicle", "character_base", "character_wear", "effect", "marker":
		return true
	default:
		return false
	}
}

func cityRealtimeVisualGenerationReviewAllowed(status, decision string) bool {
	switch decision {
	case "cancelled":
		return status == "draft" || status == "queued" || status == "generated" || status == "reviewing"
	case "approved", "rejected":
		return status == "generated" || status == "reviewing"
	default:
		return false
	}
}

func cityRealtimeVisualPackSupportsReleasePolicy(compatibility json.RawMessage, profileID string) bool {
	var decoded struct {
		SpatialProfileIDs          []string `json:"spatial_profile_ids"`
		SemanticProjectionVersions []string `json:"semantic_projection_versions"`
	}
	if json.Unmarshal(compatibility, &decoded) != nil ||
		!cityRealtimeVisualStringIncluded(decoded.SemanticProjectionVersions, cityRealtimeSemanticProjectionVersion, false) {
		return false
	}
	if profileID == "*" {
		return cityRealtimeVisualStringIncluded(decoded.SpatialProfileIDs, "*", false)
	}
	return cityRealtimeVisualStringIncluded(decoded.SpatialProfileIDs, profileID, true)
}

func insertCityRealtimeVisualPackStaging(
	ctx context.Context,
	tx *sql.Tx,
	input normalizedCityRealtimeVisualPackInput,
) (*CityRealtimeVisualPackDetail, error) {
	item, err := scanCityRealtimeVisualPackDetail(tx.QueryRowContext(ctx, `
INSERT INTO city_visual_packs
    (pack_id, pack_version, status, semantic_projection_version, render_contract_version,
     compatibility, manifest, manifest_hash, asset_set_hash, provenance)
VALUES ($1, $2, 'staging', $3, $4, $5::jsonb, $6::jsonb,
        encode(sha256(convert_to(($6::jsonb)::text, 'UTF8')), 'hex'),
        city_visual_pack_asset_set_hash($1, $2),
        jsonb_build_object(
            'source_kind', 'admin_staging',
            'rights', 'pending_review',
            'renderer_contract', 'procedural_pixel_v1'
        ))
RETURNING pack_id, pack_version, status, semantic_projection_version,
          render_contract_version, manifest_hash, asset_set_hash, compatibility,
          created_at, published_at, manifest, provenance`,
		input.packID, input.packVersion, cityRealtimeSemanticProjectionVersion,
		cityRealtimeProceduralPixelRenderContract, string(input.compatibility), string(input.manifest),
	))
	if err != nil {
		return nil, mapCityRealtimeVisualPackMutationError(err, "create")
	}
	return item, nil
}

func updateCityRealtimeVisualPackStaging(
	ctx context.Context,
	tx *sql.Tx,
	input normalizedCityRealtimeVisualPackInput,
) (*CityRealtimeVisualPackDetail, error) {
	item, err := scanCityRealtimeVisualPackDetail(tx.QueryRowContext(ctx, `
UPDATE city_visual_packs
SET compatibility = $1::jsonb,
    manifest = $2::jsonb,
    manifest_hash = encode(sha256(convert_to(($2::jsonb)::text, 'UTF8')), 'hex'),
    asset_set_hash = city_visual_pack_asset_set_hash(pack_id, pack_version)
WHERE pack_id = $3
  AND pack_version = $4
  AND status = 'staging'
RETURNING pack_id, pack_version, status, semantic_projection_version,
          render_contract_version, manifest_hash, asset_set_hash, compatibility,
          created_at, published_at, manifest, provenance`,
		string(input.compatibility), string(input.manifest), input.packID, input.packVersion,
	))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrCityVisualPackNotStaging
	}
	if err != nil {
		return nil, mapCityRealtimeVisualPackMutationError(err, "update")
	}
	return item, nil
}

func publishCityRealtimeVisualPack(
	ctx context.Context,
	tx *sql.Tx,
	packID, packVersion string,
) (*CityRealtimeVisualPackDetail, error) {
	item, err := scanCityRealtimeVisualPackDetail(tx.QueryRowContext(ctx, `
UPDATE city_visual_packs
SET status = 'published',
    published_at = NOW(),
    asset_set_hash = city_visual_pack_asset_set_hash(pack_id, pack_version)
WHERE pack_id = $1
  AND pack_version = $2
  AND status = 'staging'
RETURNING pack_id, pack_version, status, semantic_projection_version,
          render_contract_version, manifest_hash, asset_set_hash, compatibility,
          created_at, published_at, manifest, provenance`, packID, packVersion))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrCityVisualPackNotStaging
	}
	if err != nil {
		return nil, mapCityRealtimeVisualPackMutationError(err, "publish")
	}
	return item, nil
}

func retireCityRealtimeVisualPack(
	ctx context.Context,
	tx *sql.Tx,
	packID, packVersion string,
) (*CityRealtimeVisualPackDetail, error) {
	item, err := scanCityRealtimeVisualPackDetail(tx.QueryRowContext(ctx, `
UPDATE city_visual_packs
SET status = 'retired'
WHERE pack_id = $1
  AND pack_version = $2
  AND status = 'published'
RETURNING pack_id, pack_version, status, semantic_projection_version,
          render_contract_version, manifest_hash, asset_set_hash, compatibility,
          created_at, published_at, manifest, provenance`, packID, packVersion))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrCityVisualPackNotStaging
	}
	if err != nil {
		return nil, mapCityRealtimeVisualPackMutationError(err, "retire")
	}
	return item, nil
}

func loadCityRealtimeVisualPackDetail(
	ctx context.Context,
	queryer citySQLQueryer,
	packID, packVersion string,
	forUpdate bool,
) (*CityRealtimeVisualPackDetail, error) {
	query := `
SELECT pack_id, pack_version, status, semantic_projection_version,
       render_contract_version, manifest_hash, asset_set_hash, compatibility,
       created_at, published_at, manifest, provenance
FROM city_visual_packs
WHERE pack_id = $1 AND pack_version = $2`
	if forUpdate {
		query += " FOR UPDATE"
	}
	item, err := scanCityRealtimeVisualPackDetail(queryer.QueryRowContext(ctx, query, packID, packVersion))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrCityVisualPackNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("load city visual pack: %w", err)
	}
	return item, nil
}

func scanCityRealtimeVisualPackSummary(row cityScannable) (*CityRealtimeVisualPackSummary, error) {
	item := &CityRealtimeVisualPackSummary{}
	var compatibility []byte
	var publishedAt sql.NullTime
	if err := row.Scan(
		&item.PackID, &item.PackVersion, &item.Status, &item.SemanticProjectionVersion,
		&item.RenderContractVersion, &item.ManifestHash, &item.AssetSetHash, &compatibility,
		&item.CreatedAt, &publishedAt,
	); err != nil {
		return nil, err
	}
	item.Compatibility = cloneCityRealtimeVisualJSON(compatibility)
	item.CreatedAt = item.CreatedAt.UTC().Truncate(time.Microsecond)
	item.PublishedAt = cityRealtimeVisualNullTimePointer(publishedAt)
	return item, nil
}

func scanCityRealtimeVisualPackDetail(row cityScannable) (*CityRealtimeVisualPackDetail, error) {
	item := &CityRealtimeVisualPackDetail{}
	var compatibility, manifest, provenance []byte
	var publishedAt sql.NullTime
	if err := row.Scan(
		&item.PackID, &item.PackVersion, &item.Status, &item.SemanticProjectionVersion,
		&item.RenderContractVersion, &item.ManifestHash, &item.AssetSetHash, &compatibility,
		&item.CreatedAt, &publishedAt, &manifest, &provenance,
	); err != nil {
		return nil, err
	}
	item.Compatibility = cloneCityRealtimeVisualJSON(compatibility)
	item.Manifest = cloneCityRealtimeVisualJSON(manifest)
	item.Provenance = cloneCityRealtimeVisualJSON(provenance)
	item.CreatedAt = item.CreatedAt.UTC().Truncate(time.Microsecond)
	item.PublishedAt = cityRealtimeVisualNullTimePointer(publishedAt)
	return item, nil
}

func loadCityRealtimeVisualGenerationJobForUpdate(
	ctx context.Context,
	tx *sql.Tx,
	packID, packVersion string,
	jobID int64,
) (*CityRealtimeVisualGenerationJob, error) {
	item, err := scanCityRealtimeVisualGenerationJob(tx.QueryRowContext(ctx, `
SELECT id, asset_class, status, request_spec, candidate_hash, review,
       created_by_user_id, reviewed_by_user_id, created_at, reviewed_at
FROM city_visual_generation_jobs
WHERE id = $1 AND pack_id = $2 AND pack_version = $3
FOR UPDATE`, jobID, packID, packVersion), packID, packVersion)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrCityVisualGenerationJobNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("load city visual generation job: %w", err)
	}
	return item, nil
}

func scanCityRealtimeVisualGenerationJob(
	row cityScannable,
	packID, packVersion string,
) (*CityRealtimeVisualGenerationJob, error) {
	item := &CityRealtimeVisualGenerationJob{PackID: packID, PackVersion: packVersion}
	var requestSpec, review []byte
	var candidateHash sql.NullString
	var createdBy, reviewedBy sql.NullInt64
	var reviewedAt sql.NullTime
	if err := row.Scan(
		&item.ID, &item.AssetClass, &item.Status, &requestSpec, &candidateHash, &review,
		&createdBy, &reviewedBy, &item.CreatedAt, &reviewedAt,
	); err != nil {
		return nil, err
	}
	item.RequestSpec = cloneCityRealtimeVisualJSON(requestSpec)
	item.Review = cloneCityRealtimeVisualJSON(review)
	item.CandidateHash = cityRealtimeVisualNullStringPointer(candidateHash)
	item.CreatedByUserID = cityRealtimeVisualNullInt64Pointer(createdBy)
	item.ReviewedByUserID = cityRealtimeVisualNullInt64Pointer(reviewedBy)
	item.CreatedAt = item.CreatedAt.UTC().Truncate(time.Microsecond)
	item.ReviewedAt = cityRealtimeVisualNullTimePointer(reviewedAt)
	return item, nil
}

func recordCityRealtimeVisualReviewEvent(
	ctx context.Context,
	tx *sql.Tx,
	packID, packVersion string,
	generationJobID *int64,
	eventType string,
	actorUserID int64,
	metadata map[string]string,
) error {
	if actorUserID <= 0 || tx == nil {
		return ErrCityInvalidInput
	}
	raw, err := json.Marshal(metadata)
	if err != nil {
		return fmt.Errorf("encode city visual review event: %w", err)
	}
	var jobID any
	if generationJobID != nil {
		jobID = *generationJobID
	}
	if _, err = tx.ExecContext(ctx, `
INSERT INTO city_visual_pack_review_events
    (pack_id, pack_version, generation_job_id, event_type, actor_user_id, metadata)
VALUES ($1, $2, $3, $4, $5, $6::jsonb)`,
		packID, packVersion, jobID, eventType, actorUserID, string(raw)); err != nil {
		return fmt.Errorf("record city visual review event: %w", err)
	}
	return nil
}

func mapCityRealtimeVisualPackMutationError(err error, operation string) error {
	if err == nil {
		return nil
	}
	message := strings.ToLower(err.Error())
	if strings.Contains(message, "generation jobs awaiting secure asset materialisation") ||
		strings.Contains(message, "asset set hash mismatch") ||
		strings.Contains(message, "contains non-published assets") {
		return ErrCityVisualPackPublicationBlocked
	}
	return fmt.Errorf("%s city visual pack: %w", operation, err)
}

func cloneCityRealtimeVisualJSON(raw []byte) json.RawMessage {
	return append(json.RawMessage(nil), raw...)
}

func cityRealtimeVisualNullTimePointer(value sql.NullTime) *time.Time {
	if !value.Valid {
		return nil
	}
	result := value.Time.UTC().Truncate(time.Microsecond)
	return &result
}

func cityRealtimeVisualNullInt64Pointer(value sql.NullInt64) *int64 {
	if !value.Valid {
		return nil
	}
	result := value.Int64
	return &result
}

func cityRealtimeVisualNullStringPointer(value sql.NullString) *string {
	if !value.Valid {
		return nil
	}
	result := value.String
	return &result
}
