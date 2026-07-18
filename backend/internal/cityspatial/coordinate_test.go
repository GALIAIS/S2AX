package cityspatial

import (
	"errors"
	"math"
	"math/rand"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSplitWorldCoordinateUsesFloorDivisionAcrossZero(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		world int64
		chunk int64
		local int32
	}{
		{world: 0, chunk: 0, local: 0},
		{world: 31, chunk: 0, local: 31},
		{world: 32, chunk: 1, local: 0},
		{world: 33, chunk: 1, local: 1},
		{world: -1, chunk: -1, local: 31},
		{world: -31, chunk: -1, local: 1},
		{world: -32, chunk: -1, local: 0},
		{world: -33, chunk: -2, local: 31},
	}
	for _, testCase := range testCases {
		address, err := SplitWorldCoordinate(WorldCoordinate{X: testCase.world, Y: testCase.world, Z: 0}, DefaultChunkSize)
		require.NoError(t, err)
		require.Equal(t, testCase.chunk, address.Chunk.X)
		require.Equal(t, testCase.chunk, address.Chunk.Y)
		require.Equal(t, testCase.local, address.Local.X)
		require.Equal(t, testCase.local, address.Local.Y)
	}
}

func TestCellAddressRoundTripForRandomSignedCoordinates(t *testing.T) {
	t.Parallel()
	random := rand.New(rand.NewSource(20260718))
	for index := 0; index < 20_000; index++ {
		coordinate := WorldCoordinate{
			X: random.Int63n(2_000_000_001) - 1_000_000_000,
			Y: random.Int63n(2_000_000_001) - 1_000_000_000,
			Z: int32(random.Intn(int(MaximumZ-MinimumZ+1))) + MinimumZ,
		}
		address, err := SplitWorldCoordinate(coordinate, DefaultChunkSize)
		require.NoError(t, err)
		require.GreaterOrEqual(t, address.Local.X, int32(0))
		require.Less(t, address.Local.X, int32(DefaultChunkSize))
		require.GreaterOrEqual(t, address.Local.Y, int32(0))
		require.Less(t, address.Local.Y, int32(DefaultChunkSize))

		restored, err := JoinCellAddress(address, DefaultChunkSize)
		require.NoError(t, err)
		require.Equal(t, coordinate, restored)
	}
}

func TestCoordinateBoundariesAndOverflowAreRejected(t *testing.T) {
	t.Parallel()
	_, err := SplitWorldCoordinate(WorldCoordinate{Z: MinimumZ - 1}, DefaultChunkSize)
	require.ErrorIs(t, err, ErrCoordinateRange)

	_, err = SplitWorldCoordinate(WorldCoordinate{}, 0)
	require.ErrorIs(t, err, ErrInvalidChunkSize)

	_, err = JoinCellAddress(CellAddress{
		Chunk: ChunkCoordinate{X: math.MaxInt64, Z: 0},
		Local: LocalCoordinate{},
	}, DefaultChunkSize)
	require.ErrorIs(t, err, ErrCoordinateRange)

	_, err = JoinCellAddress(CellAddress{
		Chunk: ChunkCoordinate{X: math.MinInt64, Z: 0},
		Local: LocalCoordinate{},
	}, DefaultChunkSize)
	require.ErrorIs(t, err, ErrCoordinateRange)

	_, err = JoinCellAddress(CellAddress{
		Chunk: ChunkCoordinate{Z: 0},
		Local: LocalCoordinate{X: int32(DefaultChunkSize)},
	}, DefaultChunkSize)
	require.ErrorIs(t, err, ErrInvalidLocalCell)

	require.True(t, errors.Is(ValidateZ(MaximumZ+1, MinimumZ, MaximumZ), ErrCoordinateRange))
}

func TestChunkBoundsAndStableKey(t *testing.T) {
	t.Parallel()
	coordinate := ChunkCoordinate{X: -2, Y: 3, Z: -1}
	bounds, err := BoundsForChunk(coordinate, DefaultChunkSize)
	require.NoError(t, err)
	require.Equal(t, WorldCoordinate{X: -64, Y: 96, Z: -1}, bounds.Minimum)
	require.Equal(t, WorldCoordinate{X: -33, Y: 127, Z: -1}, bounds.Maximum)
	require.Equal(t, "z:-1/x:-2/y:3", StableChunkKey(coordinate))

	minimumAddress, err := SplitWorldCoordinate(bounds.Minimum, DefaultChunkSize)
	require.NoError(t, err)
	require.Equal(t, coordinate, minimumAddress.Chunk)
	require.Equal(t, LocalCoordinate{}, minimumAddress.Local)

	maximumAddress, err := SplitWorldCoordinate(bounds.Maximum, DefaultChunkSize)
	require.NoError(t, err)
	require.Equal(t, coordinate, maximumAddress.Chunk)
	require.Equal(t, LocalCoordinate{X: 31, Y: 31}, maximumAddress.Local)
}
