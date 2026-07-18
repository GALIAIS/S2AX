package cityspatial

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
)

const (
	DefaultGeneratorID      = "sub2api-classic-mapgen"
	DefaultGeneratorVersion = "1.0.0"
	DefaultOvermapMinimum   = int64(-4)
	DefaultOvermapMaximum   = int64(4)
	SurfaceZ                = int32(0)
	ChunkPayloadFormat      = "city-chunk-v1"
)

const (
	ConnectionNorth uint8 = 1
	ConnectionEast  uint8 = 2
	ConnectionSouth uint8 = 4
	ConnectionWest  uint8 = 8
)

var (
	ErrInvalidGenerationInput = errors.New("invalid city spatial generation input")
	ErrInvalidChunkPayload    = errors.New("invalid city spatial chunk payload")
)

var requiredDistrictCodes = []string{"central", "east", "harbor", "north", "south", "west"}

type GeneratorBinding struct {
	SimulationVersion string `json:"simulation_version"`
	WorldSeed         int64  `json:"world_seed"`
	RuleSetID         string `json:"rule_set_id"`
	RuleSetVersion    string `json:"rule_set_version"`
	RuleSetHash       string `json:"rule_set_hash"`
	GeneratorID       string `json:"generator_id"`
	GeneratorVersion  string `json:"generator_version"`
}

type OvermapTile struct {
	ChunkX       int64  `json:"chunk_x"`
	ChunkY       int64  `json:"chunk_y"`
	Z            int32  `json:"z"`
	DistrictCode string `json:"district_code"`
	TerrainID    string `json:"terrain_id"`
	RoadMask     uint8  `json:"road_mask"`
	RiverMask    uint8  `json:"river_mask"`
	Variant      int    `json:"variant"`
	TileHash     string `json:"tile_hash"`
}

type Overmap struct {
	MinimumChunkX int64         `json:"minimum_chunk_x"`
	MaximumChunkX int64         `json:"maximum_chunk_x"`
	MinimumChunkY int64         `json:"minimum_chunk_y"`
	MaximumChunkY int64         `json:"maximum_chunk_y"`
	Z             int32         `json:"z"`
	SeedProof     string        `json:"seed_proof"`
	RootHash      string        `json:"root_hash"`
	Tiles         []OvermapTile `json:"tiles"`
}

type TerrainRun struct {
	DefinitionID string `json:"definition_id"`
	Length       int    `json:"length"`
}

type FurnitureCell struct {
	X            int32  `json:"x"`
	Y            int32  `json:"y"`
	DefinitionID string `json:"definition_id"`
}

type ChunkPayload struct {
	Format      string          `json:"format"`
	Width       int             `json:"width"`
	Height      int             `json:"height"`
	TerrainRuns []TerrainRun    `json:"terrain_runs"`
	Furniture   []FurnitureCell `json:"furniture"`
}

type GeneratedChunk struct {
	Coordinate       ChunkCoordinate `json:"coordinate"`
	DistrictCode     string          `json:"district_code"`
	GenerationProof  string          `json:"generation_proof"`
	PayloadHash      string          `json:"payload_hash"`
	Payload          ChunkPayload    `json:"payload"`
	CanonicalPayload []byte          `json:"-"`
}

func DefaultGeneratorBinding(simulationVersion string, worldSeed int64, ruleSet *RuleSet) (GeneratorBinding, error) {
	if ruleSet == nil || strings.TrimSpace(simulationVersion) == "" || worldSeed <= 0 ||
		ruleSet.ID == "" || ruleSet.Version == "" || len(ruleSet.ContentHash) != sha256.Size*2 ||
		ruleSet.ChunkSize != DefaultChunkSize {
		return GeneratorBinding{}, ErrInvalidGenerationInput
	}
	return GeneratorBinding{
		SimulationVersion: strings.TrimSpace(simulationVersion), WorldSeed: worldSeed,
		RuleSetID: ruleSet.ID, RuleSetVersion: ruleSet.Version, RuleSetHash: ruleSet.ContentHash,
		GeneratorID: DefaultGeneratorID, GeneratorVersion: DefaultGeneratorVersion,
	}, nil
}

func GenerateDefaultOvermap(binding GeneratorBinding, districtCodes []string) (*Overmap, error) {
	if err := validateGeneratorBinding(binding); err != nil {
		return nil, err
	}
	if !sameStringSet(districtCodes, requiredDistrictCodes) {
		return nil, fmt.Errorf("%w: district catalog", ErrInvalidGenerationInput)
	}
	verticalRoad := int64(deriveGenerationByte(binding, "overmap.vertical_road")%3) - 1
	horizontalRoad := int64(deriveGenerationByte(binding, "overmap.horizontal_road")%3) - 1
	riverX := DefaultOvermapMaximum
	if deriveGenerationByte(binding, "overmap.river_side")&1 == 0 {
		riverX = DefaultOvermapMinimum
	}

	tiles := make([]OvermapTile, 0, 81)
	for y := DefaultOvermapMinimum; y <= DefaultOvermapMaximum; y++ {
		for x := DefaultOvermapMinimum; x <= DefaultOvermapMaximum; x++ {
			tile := OvermapTile{
				ChunkX: x, ChunkY: y, Z: SurfaceZ,
				DistrictCode: overmapDistrictCode(x, y, verticalRoad, horizontalRoad, riverX),
				TerrainID:    overmapTerrainID(binding, x, y, riverX),
				Variant:      int(deriveGenerationByte(binding, fmt.Sprintf("overmap.variant/%d/%d", x, y)) % 4),
			}
			if x == verticalRoad {
				tile.RoadMask |= ConnectionNorth | ConnectionSouth
			}
			if y == horizontalRoad {
				tile.RoadMask |= ConnectionEast | ConnectionWest
			}
			if x == riverX {
				tile.RiverMask = ConnectionNorth | ConnectionSouth
			}
			var err error
			tile.TileHash, err = hashOvermapTile(tile)
			if err != nil {
				return nil, err
			}
			tiles = append(tiles, tile)
		}
	}
	overmap := &Overmap{
		MinimumChunkX: DefaultOvermapMinimum, MaximumChunkX: DefaultOvermapMaximum,
		MinimumChunkY: DefaultOvermapMinimum, MaximumChunkY: DefaultOvermapMaximum,
		Z: SurfaceZ, SeedProof: deriveGenerationHex(binding, "overmap.seed_proof"), Tiles: tiles,
	}
	rootPayload, err := json.Marshal(struct {
		Binding GeneratorBinding `json:"binding"`
		Tiles   []OvermapTile    `json:"tiles"`
	}{Binding: binding, Tiles: tiles})
	if err != nil {
		return nil, fmt.Errorf("marshal city overmap root: %w", err)
	}
	overmap.RootHash = sha256Hex(rootPayload)
	return overmap, nil
}

func GenerateDefaultChunk(binding GeneratorBinding, ruleSet *RuleSet, tile OvermapTile) (*GeneratedChunk, error) {
	if err := validateGeneratorBinding(binding); err != nil {
		return nil, err
	}
	if ruleSet == nil || ruleSet.ID != binding.RuleSetID || ruleSet.Version != binding.RuleSetVersion ||
		ruleSet.ContentHash != binding.RuleSetHash || tile.Z != SurfaceZ ||
		tile.ChunkX < DefaultOvermapMinimum || tile.ChunkX > DefaultOvermapMaximum ||
		tile.ChunkY < DefaultOvermapMinimum || tile.ChunkY > DefaultOvermapMaximum {
		return nil, ErrInvalidGenerationInput
	}
	expectedTileHash, err := hashOvermapTile(tile)
	if err != nil || expectedTileHash != tile.TileHash {
		return nil, fmt.Errorf("%w: overmap tile hash", ErrInvalidGenerationInput)
	}

	cellCount := int(DefaultChunkSize * DefaultChunkSize)
	cells := make([]string, cellCount)
	for index := range cells {
		cells[index] = tile.TerrainID
	}
	if tile.RiverMask == 0 {
		paintRoads(cells, tile.RoadMask)
	}
	furniture := generateChunkFurniture(binding, tile, cells)
	payload := ChunkPayload{
		Format: ChunkPayloadFormat, Width: int(DefaultChunkSize), Height: int(DefaultChunkSize),
		TerrainRuns: encodeTerrainRuns(cells), Furniture: furniture,
	}
	if err = ValidateChunkPayload(ruleSet, payload); err != nil {
		return nil, err
	}
	canonical, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal city chunk payload: %w", err)
	}
	coordinate := ChunkCoordinate{X: tile.ChunkX, Y: tile.ChunkY, Z: tile.Z}
	proofNamespace := fmt.Sprintf("chunk.proof/%d/%d/%d/%s", coordinate.X, coordinate.Y, coordinate.Z, tile.TileHash)
	return &GeneratedChunk{
		Coordinate: coordinate, DistrictCode: tile.DistrictCode,
		GenerationProof: deriveGenerationHex(binding, proofNamespace),
		PayloadHash:     sha256Hex(canonical), Payload: payload, CanonicalPayload: canonical,
	}, nil
}

func ValidateChunkPayload(ruleSet *RuleSet, payload ChunkPayload) error {
	if ruleSet == nil || payload.Format != ChunkPayloadFormat ||
		payload.Width != int(DefaultChunkSize) || payload.Height != int(DefaultChunkSize) ||
		len(payload.TerrainRuns) == 0 {
		return ErrInvalidChunkPayload
	}
	definitions := make(map[string]RuleKind, len(ruleSet.Definitions))
	for _, definition := range ruleSet.Definitions {
		definitions[definition.ID] = definition.Kind
	}
	total := 0
	previous := ""
	for _, run := range payload.TerrainRuns {
		if run.Length <= 0 || run.DefinitionID == previous || definitions[run.DefinitionID] != RuleKindTerrain {
			return ErrInvalidChunkPayload
		}
		total += run.Length
		if total > payload.Width*payload.Height {
			return ErrInvalidChunkPayload
		}
		previous = run.DefinitionID
	}
	if total != payload.Width*payload.Height {
		return ErrInvalidChunkPayload
	}
	lastY, lastX, lastDefinition := int32(-1), int32(-1), ""
	for _, item := range payload.Furniture {
		if item.X < 0 || item.X >= int32(payload.Width) || item.Y < 0 || item.Y >= int32(payload.Height) ||
			definitions[item.DefinitionID] != RuleKindFurniture {
			return ErrInvalidChunkPayload
		}
		if item.Y < lastY || item.Y == lastY && (item.X < lastX || item.X == lastX && item.DefinitionID <= lastDefinition) {
			return ErrInvalidChunkPayload
		}
		if item.Y == lastY && item.X == lastX {
			return ErrInvalidChunkPayload
		}
		lastY, lastX, lastDefinition = item.Y, item.X, item.DefinitionID
	}
	return nil
}

func validateGeneratorBinding(binding GeneratorBinding) error {
	if strings.TrimSpace(binding.SimulationVersion) == "" || binding.WorldSeed <= 0 ||
		binding.RuleSetID == "" || binding.RuleSetVersion == "" || len(binding.RuleSetHash) != sha256.Size*2 ||
		binding.GeneratorID != DefaultGeneratorID || binding.GeneratorVersion != DefaultGeneratorVersion {
		return ErrInvalidGenerationInput
	}
	if _, err := hex.DecodeString(binding.RuleSetHash); err != nil {
		return ErrInvalidGenerationInput
	}
	return nil
}

func hashOvermapTile(tile OvermapTile) (string, error) {
	tile.TileHash = ""
	if tile.Z != SurfaceZ || tile.ChunkX < DefaultOvermapMinimum || tile.ChunkX > DefaultOvermapMaximum ||
		tile.ChunkY < DefaultOvermapMinimum || tile.ChunkY > DefaultOvermapMaximum ||
		tile.DistrictCode == "" || tile.TerrainID == "" || tile.Variant < 0 || tile.Variant > 3 ||
		tile.RoadMask > 15 || tile.RiverMask > 15 {
		return "", ErrInvalidGenerationInput
	}
	raw, err := json.Marshal(tile)
	if err != nil {
		return "", fmt.Errorf("marshal city overmap tile: %w", err)
	}
	return sha256Hex(raw), nil
}

func overmapDistrictCode(x, y, verticalRoad, horizontalRoad, riverX int64) string {
	if absInt64(x-verticalRoad) <= 1 && absInt64(y-horizontalRoad) <= 1 {
		return "central"
	}
	if y < horizontalRoad-1 {
		return "north"
	}
	if y > horizontalRoad+1 {
		if x == riverX || riverX > 0 && x >= 2 || riverX < 0 && x <= -2 {
			return "harbor"
		}
		return "south"
	}
	if x > verticalRoad {
		return "east"
	}
	return "west"
}

func overmapTerrainID(binding GeneratorBinding, x, y, riverX int64) string {
	if x == riverX {
		return "terrain.deep_water"
	}
	if deriveGenerationByte(binding, fmt.Sprintf("overmap.terrain/%d/%d", x, y))%5 == 0 {
		return "terrain.soil"
	}
	return "terrain.grass"
}

func paintRoads(cells []string, mask uint8) {
	if mask == 0 {
		return
	}
	const centerStart, centerEnd = 14, 17
	paint := func(x, y int, definition string) {
		if x >= 0 && x < int(DefaultChunkSize) && y >= 0 && y < int(DefaultChunkSize) {
			cells[y*int(DefaultChunkSize)+x] = definition
		}
	}
	for lane := centerStart - 1; lane <= centerEnd+1; lane++ {
		if mask&(ConnectionNorth|ConnectionSouth) != 0 {
			start, end := centerStart, centerEnd
			if mask&ConnectionNorth != 0 {
				start = 0
			}
			if mask&ConnectionSouth != 0 {
				end = int(DefaultChunkSize) - 1
			}
			for y := start; y <= end; y++ {
				paint(lane, y, "terrain.sidewalk")
			}
		}
		if mask&(ConnectionEast|ConnectionWest) != 0 {
			start, end := centerStart, centerEnd
			if mask&ConnectionWest != 0 {
				start = 0
			}
			if mask&ConnectionEast != 0 {
				end = int(DefaultChunkSize) - 1
			}
			for x := start; x <= end; x++ {
				paint(x, lane, "terrain.sidewalk")
			}
		}
	}
	for lane := centerStart; lane <= centerEnd; lane++ {
		if mask&(ConnectionNorth|ConnectionSouth) != 0 {
			start, end := centerStart, centerEnd
			if mask&ConnectionNorth != 0 {
				start = 0
			}
			if mask&ConnectionSouth != 0 {
				end = int(DefaultChunkSize) - 1
			}
			for y := start; y <= end; y++ {
				paint(lane, y, "terrain.road")
			}
		}
		if mask&(ConnectionEast|ConnectionWest) != 0 {
			start, end := centerStart, centerEnd
			if mask&ConnectionWest != 0 {
				start = 0
			}
			if mask&ConnectionEast != 0 {
				end = int(DefaultChunkSize) - 1
			}
			for x := start; x <= end; x++ {
				paint(x, lane, "terrain.road")
			}
		}
	}
}

func generateChunkFurniture(binding GeneratorBinding, tile OvermapTile, cells []string) []FurnitureCell {
	if tile.RiverMask != 0 {
		return []FurnitureCell{}
	}
	result := make([]FurnitureCell, 0, 20)
	for y := int32(0); y < int32(DefaultChunkSize); y++ {
		for x := int32(0); x < int32(DefaultChunkSize); x++ {
			terrain := cells[int(y)*int(DefaultChunkSize)+int(x)]
			if terrain != "terrain.grass" || deriveGenerationByte(binding,
				fmt.Sprintf("chunk.tree/%d/%d/%d/%d", tile.ChunkX, tile.ChunkY, x, y))%53 != 0 {
				continue
			}
			result = append(result, FurnitureCell{X: x, Y: y, DefinitionID: "furniture.tree"})
		}
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Y != result[j].Y {
			return result[i].Y < result[j].Y
		}
		if result[i].X != result[j].X {
			return result[i].X < result[j].X
		}
		return result[i].DefinitionID < result[j].DefinitionID
	})
	return result
}

func encodeTerrainRuns(cells []string) []TerrainRun {
	runs := make([]TerrainRun, 0, 64)
	for _, definitionID := range cells {
		if len(runs) > 0 && runs[len(runs)-1].DefinitionID == definitionID {
			runs[len(runs)-1].Length++
			continue
		}
		runs = append(runs, TerrainRun{DefinitionID: definitionID, Length: 1})
	}
	return runs
}

func deriveGenerationByte(binding GeneratorBinding, namespace string) byte {
	digest := deriveGenerationDigest(binding, namespace)
	return digest[0]
}

func deriveGenerationHex(binding GeneratorBinding, namespace string) string {
	digest := deriveGenerationDigest(binding, namespace)
	return hex.EncodeToString(digest[:])
}

func deriveGenerationDigest(binding GeneratorBinding, namespace string) [sha256.Size]byte {
	hasher := sha256.New()
	writeGenerationString(hasher, binding.SimulationVersion)
	writeGenerationInt64(hasher, binding.WorldSeed)
	writeGenerationString(hasher, binding.RuleSetID)
	writeGenerationString(hasher, binding.RuleSetVersion)
	writeGenerationString(hasher, binding.RuleSetHash)
	writeGenerationString(hasher, binding.GeneratorID)
	writeGenerationString(hasher, binding.GeneratorVersion)
	writeGenerationString(hasher, namespace)
	var digest [sha256.Size]byte
	copy(digest[:], hasher.Sum(nil))
	return digest
}

func writeGenerationString(hasher interface{ Write([]byte) (int, error) }, value string) {
	var size [4]byte
	binary.BigEndian.PutUint32(size[:], uint32(len(value)))
	_, _ = hasher.Write(size[:])
	_, _ = hasher.Write([]byte(value))
}

func writeGenerationInt64(hasher interface{ Write([]byte) (int, error) }, value int64) {
	var raw [8]byte
	binary.BigEndian.PutUint64(raw[:], uint64(value))
	_, _ = hasher.Write(raw[:])
}

func sha256Hex(raw []byte) string {
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:])
}

func sameStringSet(actual, expected []string) bool {
	if len(actual) != len(expected) {
		return false
	}
	copyActual := append([]string(nil), actual...)
	copyExpected := append([]string(nil), expected...)
	sort.Strings(copyActual)
	sort.Strings(copyExpected)
	for index := range copyActual {
		if copyActual[index] != copyExpected[index] || index > 0 && copyActual[index] == copyActual[index-1] {
			return false
		}
	}
	return true
}

func absInt64(value int64) int64 {
	if value < 0 {
		return -value
	}
	return value
}
