package cityspatial

import (
	"errors"
	"fmt"
	"math"
)

const (
	DefaultChunkSize int64 = 32
	MinimumZ         int32 = -32
	MaximumZ         int32 = 127
)

var (
	ErrInvalidChunkSize = errors.New("city spatial chunk size must be positive")
	ErrInvalidLocalCell = errors.New("city spatial local cell is outside the chunk")
	ErrCoordinateRange  = errors.New("city spatial coordinate is outside the supported range")
)

// WorldCoordinate is the authoritative discrete cell position. X grows east,
// Y grows south, and Z grows upward from the surface level at zero.
type WorldCoordinate struct {
	X int64 `json:"x"`
	Y int64 `json:"y"`
	Z int32 `json:"z"`
}

// ChunkCoordinate identifies one two-dimensional chunk at one discrete Z level.
type ChunkCoordinate struct {
	X int64 `json:"x"`
	Y int64 `json:"y"`
	Z int32 `json:"z"`
}

type LocalCoordinate struct {
	X int32 `json:"x"`
	Y int32 `json:"y"`
}

type CellAddress struct {
	Chunk ChunkCoordinate `json:"chunk"`
	Local LocalCoordinate `json:"local"`
}

type ChunkBounds struct {
	Minimum WorldCoordinate `json:"minimum"`
	Maximum WorldCoordinate `json:"maximum"`
}

func SplitWorldCoordinate(coordinate WorldCoordinate, chunkSize int64) (CellAddress, error) {
	if err := ValidateZ(coordinate.Z, MinimumZ, MaximumZ); err != nil {
		return CellAddress{}, err
	}
	chunkX, localX, err := splitAxis(coordinate.X, chunkSize)
	if err != nil {
		return CellAddress{}, err
	}
	chunkY, localY, err := splitAxis(coordinate.Y, chunkSize)
	if err != nil {
		return CellAddress{}, err
	}
	return CellAddress{
		Chunk: ChunkCoordinate{X: chunkX, Y: chunkY, Z: coordinate.Z},
		Local: LocalCoordinate{X: int32(localX), Y: int32(localY)},
	}, nil
}

func JoinCellAddress(address CellAddress, chunkSize int64) (WorldCoordinate, error) {
	if err := ValidateZ(address.Chunk.Z, MinimumZ, MaximumZ); err != nil {
		return WorldCoordinate{}, err
	}
	if chunkSize <= 0 || chunkSize > math.MaxInt32 {
		return WorldCoordinate{}, ErrInvalidChunkSize
	}
	if address.Local.X < 0 || int64(address.Local.X) >= chunkSize ||
		address.Local.Y < 0 || int64(address.Local.Y) >= chunkSize {
		return WorldCoordinate{}, ErrInvalidLocalCell
	}
	x, err := joinAxis(address.Chunk.X, int64(address.Local.X), chunkSize)
	if err != nil {
		return WorldCoordinate{}, err
	}
	y, err := joinAxis(address.Chunk.Y, int64(address.Local.Y), chunkSize)
	if err != nil {
		return WorldCoordinate{}, err
	}
	return WorldCoordinate{X: x, Y: y, Z: address.Chunk.Z}, nil
}

func BoundsForChunk(coordinate ChunkCoordinate, chunkSize int64) (ChunkBounds, error) {
	if chunkSize <= 0 || chunkSize > math.MaxInt32 {
		return ChunkBounds{}, ErrInvalidChunkSize
	}
	minimum, err := JoinCellAddress(CellAddress{
		Chunk: coordinate,
		Local: LocalCoordinate{},
	}, chunkSize)
	if err != nil {
		return ChunkBounds{}, err
	}
	maximum, err := JoinCellAddress(CellAddress{
		Chunk: coordinate,
		Local: LocalCoordinate{X: int32(chunkSize - 1), Y: int32(chunkSize - 1)},
	}, chunkSize)
	if err != nil {
		return ChunkBounds{}, err
	}
	return ChunkBounds{Minimum: minimum, Maximum: maximum}, nil
}

func ValidateZ(z, minimum, maximum int32) error {
	if minimum > maximum || minimum < MinimumZ || maximum > MaximumZ || z < minimum || z > maximum {
		return fmt.Errorf("%w: z=%d allowed=%d..%d", ErrCoordinateRange, z, minimum, maximum)
	}
	return nil
}

func StableChunkKey(coordinate ChunkCoordinate) string {
	return fmt.Sprintf("z:%d/x:%d/y:%d", coordinate.Z, coordinate.X, coordinate.Y)
}

func splitAxis(value, chunkSize int64) (chunk, local int64, err error) {
	if chunkSize <= 0 || chunkSize > math.MaxInt32 {
		return 0, 0, ErrInvalidChunkSize
	}
	chunk = value / chunkSize
	local = value % chunkSize
	if local < 0 {
		chunk--
		local += chunkSize
	}
	return chunk, local, nil
}

func joinAxis(chunk, local, chunkSize int64) (int64, error) {
	if chunkSize <= 0 || chunkSize > math.MaxInt32 {
		return 0, ErrInvalidChunkSize
	}
	if local < 0 || local >= chunkSize {
		return 0, ErrInvalidLocalCell
	}
	if chunk > math.MaxInt64/chunkSize || chunk < math.MinInt64/chunkSize {
		return 0, ErrCoordinateRange
	}
	origin := chunk * chunkSize
	if origin > math.MaxInt64-local {
		return 0, ErrCoordinateRange
	}
	return origin + local, nil
}
