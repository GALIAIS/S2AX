package service

import (
	"context"
	"database/sql"
	"fmt"
)

// restoreCityWorldVersionVectorProjection restores only the active vector's
// bindings. Historical headers remain immutable audit records; a missing
// header is therefore an integrity failure rather than a value recovery can
// invent from a snapshot.
func restoreCityWorldVersionVectorProjection(
	ctx context.Context,
	tx *sql.Tx,
	worldID int64,
	vector CityWorldVersionVector,
) (int, error) {
	if worldID <= 0 {
		return 0, ErrCityInvalidInput
	}
	if err := validateCityWorldVersionVector(vector); err != nil {
		return 0, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "world_version_vector"}).WithCause(err)
	}
	var engineVersion string
	var baselineTick int64
	if err := tx.QueryRowContext(ctx, `
SELECT engine_version, baseline_tick
FROM city_world_version_vectors
WHERE world_id = $1 AND generation = $2
FOR UPDATE`, worldID, vector.Generation).Scan(&engineVersion, &baselineTick); err != nil {
		if err == sql.ErrNoRows {
			return 0, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "world_version_vector_header"})
		}
		return 0, fmt.Errorf("load city recovery version-vector header: %w", err)
	}
	var worldVersion string
	if err := tx.QueryRowContext(ctx, `SELECT simulation_version FROM city_worlds WHERE id = $1`, worldID).Scan(&worldVersion); err != nil {
		return 0, fmt.Errorf("load city recovery version-vector world: %w", err)
	}
	if engineVersion != worldVersion || baselineTick != vector.BaselineTick {
		return 0, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "world_version_vector_header"})
	}
	if err := activateCityWorldVersionVectorWrite(ctx, tx, worldID, vector.Generation); err != nil {
		return 0, err
	}
	result, err := tx.ExecContext(ctx, `
DELETE FROM city_world_version_bindings
WHERE world_id = $1 AND generation = $2`, worldID, vector.Generation)
	if err != nil {
		return 0, fmt.Errorf("clear city recovery version-vector bindings: %w", err)
	}
	count64, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("count cleared city recovery version-vector bindings: %w", err)
	}
	count := int(count64)
	for _, binding := range vector.Bindings {
		if _, err = tx.ExecContext(ctx, `
INSERT INTO city_world_version_bindings
    (world_id, generation, component_code, bundle_id, bundle_version, content_hash, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7::jsonb)`,
			worldID, vector.Generation, binding.ComponentCode, binding.BundleID,
			binding.BundleVersion, binding.ContentHash, []byte(binding.Metadata)); err != nil {
			return 0, fmt.Errorf("restore city version-vector component %s: %w", binding.ComponentCode, err)
		}
		count++
	}
	return count, nil
}
