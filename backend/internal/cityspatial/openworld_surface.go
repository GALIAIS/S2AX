package cityspatial

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
)

const OpenWorldChunkPayloadFormat = "city-openworld-chunk-v1"

// OpenWorldCellLayer is a persisted V2 map layer. Terrain is run-length
// encoded; sparse walls, furnishings, portals, and future actor facts stay
// explicit so a glyph renderer can faithfully stack them without generating
// anything in the browser.
type OpenWorldCellLayer struct {
	X            int32    `json:"x"`
	Y            int32    `json:"y"`
	Kind         RuleKind `json:"kind"`
	DefinitionID string   `json:"definition_id"`
}

type OpenWorldChunkPayload struct {
	Format      string               `json:"format"`
	Width       int                  `json:"width"`
	Height      int                  `json:"height"`
	TerrainRuns []TerrainRun         `json:"terrain_runs"`
	Layers      []OpenWorldCellLayer `json:"layers"`
}

type GeneratedOpenWorldChunk struct {
	Coordinate       ChunkCoordinate       `json:"coordinate"`
	Payload          OpenWorldChunkPayload `json:"payload"`
	PayloadHash      string                `json:"payload_hash"`
	CanonicalPayload []byte                `json:"-"`
}

// GeneratedOpenWorldPortal is a deterministic traversable connection between
// two V2 local-map coordinates.  It deliberately describes topology only;
// access state, locks and actor-specific eligibility belong to later runtime
// facts and must not rewrite immutable world-generation data.
type GeneratedOpenWorldPortal struct {
	Code           string        `json:"code"`
	BuildingCode   string        `json:"building_code"`
	PortalType     string        `json:"portal_type"`
	FromFloorIndex int32         `json:"from_floor_index"`
	ToFloorIndex   int32         `json:"to_floor_index"`
	From           WorldgenPoint `json:"from"`
	To             WorldgenPoint `json:"to"`
	Bidirectional  bool          `json:"bidirectional"`
}

type GeneratedOpenWorldSurfaceSector struct {
	Bounds      WorldgenBounds                      `json:"bounds"`
	PlanHash    string                              `json:"plan_hash"`
	Chunks      []GeneratedOpenWorldChunk           `json:"chunks"`
	Buildings   []GeneratedWorldgenBuilding         `json:"buildings"`
	Interiors   []GeneratedWorldgenBuildingInterior `json:"interiors"`
	Portals     []GeneratedOpenWorldPortal          `json:"portals,omitempty"`
	ContentHash string                              `json:"content_hash"`
}

// GenerateOpenWorldSurfaceSector keeps the historical one-floor materializer
// used by city-openworld-v1.  New V2 worlds use
// GenerateOpenWorldSurfaceSectorV2 below so this function can never silently
// rewrite the canonical genesis content of an existing V1 world.
func GenerateOpenWorldSurfaceSector(
	plan *GeneratedWorldgenPlan,
	bounds WorldgenBounds,
) (*GeneratedOpenWorldSurfaceSector, error) {
	return generateOpenWorldSurfaceSector(plan, bounds, false)
}

// GenerateOpenWorldSurfaceSectorV2 materializes a bounded surface sector and
// the complete interior stack of every intersecting building.  Surface chunks
// remain Z=0 (the overmap is intentionally sparse), while each floor is an
// independently hashed local-map fact.  This separates visual range loading
// from vertical simulation facts without reducing high-rise buildings to a
// browser-only illusion.
func GenerateOpenWorldSurfaceSectorV2(
	plan *GeneratedWorldgenPlan,
	bounds WorldgenBounds,
) (*GeneratedOpenWorldSurfaceSector, error) {
	return generateOpenWorldSurfaceSector(plan, bounds, true)
}

func generateOpenWorldSurfaceSector(
	plan *GeneratedWorldgenPlan,
	bounds WorldgenBounds,
	includeAllInteriorFloors bool,
) (*GeneratedOpenWorldSurfaceSector, error) {
	if plan == nil || validateGeneratedWorldgenPlan(plan) != nil ||
		validateWorldgenQueryBounds(bounds) != nil || bounds.Z != SurfaceZ ||
		bounds.MinimumChunkX < plan.Bounds.MinimumChunkX || bounds.MaximumChunkX > plan.Bounds.MaximumChunkX ||
		bounds.MinimumChunkY < plan.Bounds.MinimumChunkY || bounds.MaximumChunkY > plan.Bounds.MaximumChunkY {
		return nil, ErrInvalidWorldgenInput
	}
	terrainByChunk := make(map[WorldgenPoint]GeneratedWorldgenTerrainPatch, len(plan.Terrain))
	for _, terrain := range plan.Terrain {
		terrainByChunk[WorldgenPoint{X: terrain.ChunkX, Y: terrain.ChunkY, Z: terrain.Z}] = terrain
	}
	roadCells := worldgenRoadCells(plan.Bounds, plan.Roads)
	buildings := make([]GeneratedWorldgenBuilding, 0)
	for _, building := range plan.Buildings {
		if worldgenFootprintIntersectsBounds(building.Footprint, bounds) {
			buildings = append(buildings, cloneWorldgenBuilding(building))
		}
	}
	interiors, err := generateOpenWorldInteriors(plan.Binding, buildings, includeAllInteriorFloors)
	if err != nil {
		return nil, err
	}
	groundInteriors := make([]GeneratedWorldgenBuildingInterior, 0, len(buildings))
	for _, interior := range interiors {
		if interior.FloorIndex == 0 {
			groundInteriors = append(groundInteriors, interior)
		}
	}
	var portals []GeneratedOpenWorldPortal
	if includeAllInteriorFloors {
		portals, err = generateOpenWorldPortals(buildings, interiors)
		if err != nil {
			return nil, err
		}
	}

	sector := &GeneratedOpenWorldSurfaceSector{
		Bounds: bounds, PlanHash: plan.BaselineHash,
		Chunks:    make([]GeneratedOpenWorldChunk, 0, int((bounds.MaximumChunkX-bounds.MinimumChunkX+1)*(bounds.MaximumChunkY-bounds.MinimumChunkY+1))),
		Buildings: buildings, Interiors: interiors, Portals: portals,
	}
	for chunkY := bounds.MinimumChunkY; chunkY <= bounds.MaximumChunkY; chunkY++ {
		for chunkX := bounds.MinimumChunkX; chunkX <= bounds.MaximumChunkX; chunkX++ {
			terrain, found := terrainByChunk[WorldgenPoint{X: chunkX, Y: chunkY, Z: bounds.Z}]
			if !found {
				return nil, ErrInvalidWorldgenInput
			}
			chunk, err := generateOpenWorldSurfaceChunk(terrain, roadCells, buildings, groundInteriors)
			if err != nil {
				return nil, err
			}
			sector.Chunks = append(sector.Chunks, chunk)
		}
	}
	contentHash, err := ComputeOpenWorldSurfaceSectorHash(sector)
	if err != nil {
		return nil, err
	}
	sector.ContentHash = contentHash
	return sector, nil
}

func generateOpenWorldInteriors(
	binding WorldgenBinding,
	buildings []GeneratedWorldgenBuilding,
	includeAllFloors bool,
) ([]GeneratedWorldgenBuildingInterior, error) {
	interiors := make([]GeneratedWorldgenBuildingInterior, 0, len(buildings))
	for _, building := range buildings {
		floorCount := int32(1)
		if includeAllFloors {
			floorCount = building.FloorCount
		}
		for floorIndex := int32(0); floorIndex < floorCount; floorIndex++ {
			interior, err := GenerateWorldgenBuildingInterior(binding, building, floorIndex)
			if err != nil {
				return nil, err
			}
			interiors = append(interiors, interior)
		}
	}
	sort.Slice(interiors, func(i, j int) bool {
		if interiors[i].BuildingCode != interiors[j].BuildingCode {
			return interiors[i].BuildingCode < interiors[j].BuildingCode
		}
		return interiors[i].FloorIndex < interiors[j].FloorIndex
	})
	return interiors, nil
}

func generateOpenWorldPortals(
	buildings []GeneratedWorldgenBuilding,
	interiors []GeneratedWorldgenBuildingInterior,
) ([]GeneratedOpenWorldPortal, error) {
	interiorsByBuilding := make(map[string]map[int32]GeneratedWorldgenBuildingInterior, len(buildings))
	for _, interior := range interiors {
		floors := interiorsByBuilding[interior.BuildingCode]
		if floors == nil {
			floors = make(map[int32]GeneratedWorldgenBuildingInterior)
			interiorsByBuilding[interior.BuildingCode] = floors
		}
		if _, duplicate := floors[interior.FloorIndex]; duplicate {
			return nil, ErrInvalidWorldgenInput
		}
		floors[interior.FloorIndex] = interior
	}
	portals := make([]GeneratedOpenWorldPortal, 0, len(buildings)*2)
	for _, building := range buildings {
		floors := interiorsByBuilding[building.Code]
		if len(floors) != int(building.FloorCount) {
			return nil, ErrInvalidWorldgenInput
		}
		exterior, found := openWorldBuildingExteriorEntrance(building)
		if !found {
			return nil, ErrInvalidWorldgenInput
		}
		portals = append(portals, GeneratedOpenWorldPortal{
			Code: building.Code + ".entrance", BuildingCode: building.Code, PortalType: "entrance",
			FromFloorIndex: 0, ToFloorIndex: 0, From: exterior, To: building.Entrance, Bidirectional: true,
		})
		for floorIndex := int32(0); floorIndex+1 < building.FloorCount; floorIndex++ {
			from, fromFound := openWorldInteriorFeaturePoint(floors[floorIndex], "stairs")
			to, toFound := openWorldInteriorFeaturePoint(floors[floorIndex+1], "stairs")
			if !fromFound || !toFound {
				return nil, ErrInvalidWorldgenInput
			}
			portals = append(portals, GeneratedOpenWorldPortal{
				Code:         fmt.Sprintf("%s.stairs.%02d.%02d", building.Code, floorIndex, floorIndex+1),
				BuildingCode: building.Code, PortalType: "stairs",
				FromFloorIndex: floorIndex, ToFloorIndex: floorIndex + 1,
				From: from, To: to, Bidirectional: true,
			})
		}
	}
	sort.Slice(portals, func(i, j int) bool { return portals[i].Code < portals[j].Code })
	return portals, nil
}

func openWorldBuildingExteriorEntrance(building GeneratedWorldgenBuilding) (WorldgenPoint, bool) {
	if len(building.Footprint) == 0 {
		return WorldgenPoint{}, false
	}
	footprint := make(map[WorldgenPoint]struct{}, len(building.Footprint))
	for _, point := range building.Footprint {
		footprint[point] = struct{}{}
	}
	for _, offset := range []struct{ x, y int64 }{{0, -1}, {1, 0}, {0, 1}, {-1, 0}} {
		candidate := WorldgenPoint{X: building.Entrance.X + offset.x, Y: building.Entrance.Y + offset.y, Z: building.Entrance.Z}
		if _, inside := footprint[candidate]; !inside {
			return candidate, true
		}
	}
	return WorldgenPoint{}, false
}

func openWorldInteriorFeaturePoint(
	interior GeneratedWorldgenBuildingInterior,
	feature string,
) (WorldgenPoint, bool) {
	for _, cell := range interior.Cells {
		if cell.Feature == feature {
			return WorldgenPoint{X: cell.X, Y: cell.Y, Z: cell.Z}, true
		}
	}
	return WorldgenPoint{}, false
}

// ComputeOpenWorldPortalHash freezes one generated topology edge independently
// of its database identity and timestamps.  It is used by persistence and
// verification paths so a corrupted or accidentally mismatched portal cannot
// be hidden behind an otherwise valid sector hash row.
func ComputeOpenWorldPortalHash(portal GeneratedOpenWorldPortal) (string, error) {
	if portal.Code == "" || portal.BuildingCode == "" ||
		(portal.PortalType != "entrance" && portal.PortalType != "stairs") ||
		portal.FromFloorIndex < 0 || portal.ToFloorIndex < 0 ||
		portal.From.Z < SurfaceZ || portal.To.Z < SurfaceZ {
		return "", ErrInvalidWorldgenInput
	}
	raw, err := json.Marshal(portal)
	if err != nil {
		return "", fmt.Errorf("marshal open-world portal: %w", err)
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

func generateOpenWorldSurfaceChunk(
	terrain GeneratedWorldgenTerrainPatch,
	roadCells map[WorldgenPoint]string,
	buildings []GeneratedWorldgenBuilding,
	interiors []GeneratedWorldgenBuildingInterior,
) (GeneratedOpenWorldChunk, error) {
	cells := make([]string, DefaultChunkSize*DefaultChunkSize)
	for index := range cells {
		cells[index] = terrain.DefinitionID
	}
	chunkBounds, err := BoundsForChunk(ChunkCoordinate{X: terrain.ChunkX, Y: terrain.ChunkY, Z: terrain.Z}, DefaultChunkSize)
	if err != nil {
		return GeneratedOpenWorldChunk{}, err
	}
	for y := chunkBounds.Minimum.Y; y <= chunkBounds.Maximum.Y; y++ {
		for x := chunkBounds.Minimum.X; x <= chunkBounds.Maximum.X; x++ {
			if _, road := roadCells[WorldgenPoint{X: x, Y: y, Z: terrain.Z}]; road {
				localX := int(x - chunkBounds.Minimum.X)
				localY := int(y - chunkBounds.Minimum.Y)
				cells[localY*int(DefaultChunkSize)+localX] = "terrain.road"
			}
		}
	}

	layers := make(map[WorldgenPoint]map[RuleKind]string)
	for _, building := range buildings {
		footprint := make(map[WorldgenPoint]struct{}, len(building.Footprint))
		for _, point := range building.Footprint {
			footprint[point] = struct{}{}
		}
		for _, point := range building.Footprint {
			if point.X < chunkBounds.Minimum.X || point.X > chunkBounds.Maximum.X ||
				point.Y < chunkBounds.Minimum.Y || point.Y > chunkBounds.Maximum.Y || point.Z != terrain.Z {
				continue
			}
			if openWorldFootprintBoundary(footprint, point) {
				openWorldSetLayer(layers, point, RuleKindStructure, "structure.wall")
			}
			if point == building.Entrance {
				openWorldSetLayer(layers, point, RuleKindPortal, "portal.door_open")
			}
		}
	}
	for _, interior := range interiors {
		if err := applyOpenWorldGroundInterior(cells, layers, chunkBounds, terrain.Z, interior); err != nil {
			return GeneratedOpenWorldChunk{}, err
		}
	}
	payload := OpenWorldChunkPayload{
		Format: OpenWorldChunkPayloadFormat, Width: int(DefaultChunkSize), Height: int(DefaultChunkSize),
		TerrainRuns: encodeTerrainRuns(cells), Layers: openWorldCanonicalLayers(layers, chunkBounds),
	}
	if err := ValidateOpenWorldChunkPayload(payload); err != nil {
		return GeneratedOpenWorldChunk{}, err
	}
	canonical, err := json.Marshal(payload)
	if err != nil {
		return GeneratedOpenWorldChunk{}, fmt.Errorf("marshal open-world chunk: %w", err)
	}
	sum := sha256.Sum256(canonical)
	return GeneratedOpenWorldChunk{
		Coordinate: ChunkCoordinate{X: terrain.ChunkX, Y: terrain.ChunkY, Z: terrain.Z},
		Payload:    payload, PayloadHash: hex.EncodeToString(sum[:]), CanonicalPayload: canonical,
	}, nil
}

func applyOpenWorldGroundInterior(
	terrain []string,
	layers map[WorldgenPoint]map[RuleKind]string,
	bounds ChunkBounds,
	z int32,
	interior GeneratedWorldgenBuildingInterior,
) error {
	if interior.FloorIndex != 0 || interior.Z != z || interior.LayoutVersion != DefaultWorldgenInteriorVersion {
		return ErrInvalidWorldgenInput
	}
	for _, cell := range interior.Cells {
		if cell.Z != z {
			return ErrInvalidWorldgenInput
		}
		point := WorldgenPoint{X: cell.X, Y: cell.Y, Z: cell.Z}
		if point.X < bounds.Minimum.X || point.X > bounds.Maximum.X || point.Y < bounds.Minimum.Y || point.Y > bounds.Maximum.Y {
			continue
		}
		localX := int(point.X - bounds.Minimum.X)
		localY := int(point.Y - bounds.Minimum.Y)
		terrain[localY*int(DefaultChunkSize)+localX] = "terrain.floor"
		switch cell.Kind {
		case BuildingLayoutCellWall:
			openWorldSetLayer(layers, point, RuleKindStructure, "structure.wall")
		case BuildingLayoutCellWindow:
			openWorldSetLayer(layers, point, RuleKindStructure, "structure.window")
		case BuildingLayoutCellDoor:
			openWorldDeleteLayer(layers, point, RuleKindStructure)
			openWorldSetLayer(layers, point, RuleKindPortal, "portal.door_open")
		case BuildingLayoutCellFurniture:
			kind, definitionID, ok := openWorldInteriorFurnishing(cell.Feature)
			if !ok {
				return ErrInvalidWorldgenInput
			}
			openWorldSetLayer(layers, point, kind, definitionID)
		case BuildingLayoutCellFloor:
			if cell.Feature == "stairs" {
				openWorldSetLayer(layers, point, RuleKindPortal, "portal.stairs_up")
			} else if cell.Feature != "" {
				return ErrInvalidWorldgenInput
			}
		default:
			return ErrInvalidWorldgenInput
		}
	}
	return nil
}

func openWorldInteriorFurnishing(feature string) (RuleKind, string, bool) {
	switch feature {
	case "bed":
		return RuleKindFurniture, "furniture.bed", true
	case "chair":
		return RuleKindFurniture, "furniture.chair", true
	case "crate":
		return RuleKindItem, "item.crate", true
	case "table", "counter", "shelf", "machine":
		return RuleKindFurniture, "furniture.table", true
	default:
		return "", "", false
	}
}

func openWorldFootprintBoundary(footprint map[WorldgenPoint]struct{}, point WorldgenPoint) bool {
	for _, offset := range buildingLayoutCardinalOffsets {
		if _, inside := footprint[WorldgenPoint{X: point.X + offset.x, Y: point.Y + offset.y, Z: point.Z}]; !inside {
			return true
		}
	}
	return false
}

func openWorldSetLayer(layers map[WorldgenPoint]map[RuleKind]string, point WorldgenPoint, kind RuleKind, definitionID string) {
	stack := layers[point]
	if stack == nil {
		stack = make(map[RuleKind]string)
		layers[point] = stack
	}
	stack[kind] = definitionID
}

func openWorldDeleteLayer(layers map[WorldgenPoint]map[RuleKind]string, point WorldgenPoint, kind RuleKind) {
	stack := layers[point]
	if stack == nil {
		return
	}
	delete(stack, kind)
	if len(stack) == 0 {
		delete(layers, point)
	}
}

func openWorldCanonicalLayers(
	layers map[WorldgenPoint]map[RuleKind]string,
	bounds ChunkBounds,
) []OpenWorldCellLayer {
	result := make([]OpenWorldCellLayer, 0, len(layers)*2)
	for point, stack := range layers {
		for kind, definitionID := range stack {
			result = append(result, OpenWorldCellLayer{
				X: int32(point.X - bounds.Minimum.X), Y: int32(point.Y - bounds.Minimum.Y),
				Kind: kind, DefinitionID: definitionID,
			})
		}
	}
	sort.Slice(result, func(i, j int) bool {
		left, right := result[i], result[j]
		if left.Y != right.Y {
			return left.Y < right.Y
		}
		if left.X != right.X {
			return left.X < right.X
		}
		return openWorldLayerRank(left.Kind) < openWorldLayerRank(right.Kind)
	})
	return result
}

func openWorldLayerRank(kind RuleKind) int {
	switch kind {
	case RuleKindStructure:
		return 1
	case RuleKindFurniture:
		return 2
	case RuleKindPortal:
		return 3
	case RuleKindItem:
		return 4
	case RuleKindEntity:
		return 5
	case RuleKindField:
		return 6
	case RuleKindOverlay:
		return 7
	default:
		return 0
	}
}

func ValidateOpenWorldChunkPayload(payload OpenWorldChunkPayload) error {
	if payload.Format != OpenWorldChunkPayloadFormat || payload.Width != int(DefaultChunkSize) ||
		payload.Height != int(DefaultChunkSize) || len(payload.TerrainRuns) == 0 {
		return ErrInvalidWorldgenInput
	}
	total := 0
	previousTerrain := ""
	for _, run := range payload.TerrainRuns {
		if run.Length <= 0 || run.DefinitionID == previousTerrain {
			return ErrInvalidWorldgenInput
		}
		total += run.Length
		previousTerrain = run.DefinitionID
	}
	if total != payload.Width*payload.Height {
		return ErrInvalidWorldgenInput
	}
	lastX, lastY, lastRank := int32(-1), int32(-1), -1
	seen := make(map[string]struct{}, len(payload.Layers))
	for _, layer := range payload.Layers {
		rank := openWorldLayerRank(layer.Kind)
		if layer.X < 0 || layer.X >= int32(payload.Width) || layer.Y < 0 || layer.Y >= int32(payload.Height) ||
			layer.DefinitionID == "" || rank == 0 ||
			layer.Y < lastY || layer.Y == lastY && (layer.X < lastX || layer.X == lastX && rank <= lastRank) {
			return ErrInvalidWorldgenInput
		}
		key := fmt.Sprintf("%d/%d/%s", layer.X, layer.Y, layer.Kind)
		if _, duplicate := seen[key]; duplicate {
			return ErrInvalidWorldgenInput
		}
		seen[key] = struct{}{}
		lastX, lastY, lastRank = layer.X, layer.Y, rank
	}
	return nil
}

func ComputeOpenWorldSurfaceSectorHash(sector *GeneratedOpenWorldSurfaceSector) (string, error) {
	if sector == nil || validateWorldgenQueryBounds(sector.Bounds) != nil || sector.PlanHash == "" ||
		len(sector.Chunks) == 0 {
		return "", ErrInvalidWorldgenInput
	}
	raw, err := json.Marshal(struct {
		Bounds    WorldgenBounds                      `json:"bounds"`
		PlanHash  string                              `json:"plan_hash"`
		Chunks    []GeneratedOpenWorldChunk           `json:"chunks"`
		Buildings []GeneratedWorldgenBuilding         `json:"buildings"`
		Interiors []GeneratedWorldgenBuildingInterior `json:"interiors"`
		Portals   []GeneratedOpenWorldPortal          `json:"portals,omitempty"`
	}{
		Bounds: sector.Bounds, PlanHash: sector.PlanHash, Chunks: sector.Chunks,
		Buildings: sector.Buildings, Interiors: sector.Interiors, Portals: sector.Portals,
	})
	if err != nil {
		return "", fmt.Errorf("marshal open-world sector: %w", err)
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}
