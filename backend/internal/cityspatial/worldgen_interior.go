package cityspatial

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// DefaultWorldgenInteriorVersion identifies the deterministic, server-side
// building interior generator used by the open-world path. It is independent
// from the legacy F7 building-layout projection, so a future template or
// profile revision cannot silently rewrite an already materialized interior.
const DefaultWorldgenInteriorVersion = "1.0.0"

// GeneratedWorldgenInteriorCell is a concrete local-map fact candidate. The
// future V2 materialization transaction persists these cells; the renderer
// only projects them as CLASSIC glyphs and never invents room contents.
type GeneratedWorldgenInteriorCell struct {
	X       int64                  `json:"x"`
	Y       int64                  `json:"y"`
	Z       int32                  `json:"z"`
	Kind    BuildingLayoutCellKind `json:"kind"`
	Feature string                 `json:"feature,omitempty"`
}

// GeneratedWorldgenBuildingInterior is a single floor of a V2 building. The
// input footprint is allowed to be irregular and to cross legacy F7 chunk
// boundaries; coordinates remain world-cell coordinates throughout.
type GeneratedWorldgenBuildingInterior struct {
	BuildingCode  string                          `json:"building_code"`
	FloorIndex    int32                           `json:"floor_index"`
	Z             int32                           `json:"z"`
	LayoutVersion string                          `json:"layout_version"`
	LayoutStyle   string                          `json:"layout_style"`
	Cells         []GeneratedWorldgenInteriorCell `json:"cells"`
	ContentHash   string                          `json:"content_hash"`
}

// GenerateWorldgenBuildingInterior turns a V2 envelope into an actual
// cell-level floor plan. It deliberately consumes the existing irregular
// footprint rather than re-boxing it into a legacy 6×6 parcel. The output has
// perimeter walls/windows, an entrance, a floor connection, a room divider,
// and area-scaled furnishings, which is the data a C:DDA-style glyph view
// needs to show a dense indoor map.
func GenerateWorldgenBuildingInterior(
	binding WorldgenBinding,
	building GeneratedWorldgenBuilding,
	floorIndex int32,
) (GeneratedWorldgenBuildingInterior, error) {
	mask, bounds, entrance, err := worldgenInteriorMask(binding, building, floorIndex)
	if err != nil {
		return GeneratedWorldgenBuildingInterior{}, err
	}
	seed := worldgenInteriorSeed(binding, building, floorIndex)
	width := int(bounds.maxX - bounds.minX + 1)
	height := int(bounds.maxY - bounds.minY + 1)
	z := building.Entrance.Z + floorIndex
	frontage := worldgenInteriorFrontage(entrance, bounds, seed)
	variation := worldgenInteriorVariation(building.LayoutStyle, seed)
	protected := make(map[buildingLayoutPoint]string, 2)
	if floorIndex == 0 {
		protected[entrance] = "entrance"
	}
	if building.FloorCount > 1 {
		if stairs, found := worldgenInteriorStairPoint(mask, width, height, entrance, seed); found {
			protected[stairs] = "stair"
		}
	}

	tiles := classifyBuildingLayoutMask(mask, width, height, seed, z)
	applyBuildingLayoutPartition(tiles, width, height, building.PrimaryUse, variation, frontage, seed, z)
	applyBuildingLayoutPortalAnchors(tiles, protected)
	applyBuildingLayoutFurnishings(tiles, building.PrimaryUse, seed, z, protected)
	if !ensureBuildingLayoutConnectivity(tiles, protected) {
		return GeneratedWorldgenBuildingInterior{}, fmt.Errorf("%w: disconnected worldgen interior for %q floor %d", ErrInvalidWorldgenInput, building.Code, floorIndex)
	}

	interior := GeneratedWorldgenBuildingInterior{
		BuildingCode:  building.Code,
		FloorIndex:    floorIndex,
		Z:             z,
		LayoutVersion: DefaultWorldgenInteriorVersion,
		LayoutStyle:   building.LayoutStyle,
		Cells:         make([]GeneratedWorldgenInteriorCell, 0, len(tiles)),
	}
	for point, tile := range tiles {
		interior.Cells = append(interior.Cells, GeneratedWorldgenInteriorCell{
			X:       point.x + bounds.minX,
			Y:       point.y + bounds.minY,
			Z:       z,
			Kind:    tile.kind,
			Feature: tile.feature,
		})
	}
	sort.Slice(interior.Cells, func(i, j int) bool {
		left, right := interior.Cells[i], interior.Cells[j]
		if left.Y != right.Y {
			return left.Y < right.Y
		}
		return left.X < right.X
	})
	hash, err := ComputeWorldgenBuildingInteriorHash(&interior)
	if err != nil {
		return GeneratedWorldgenBuildingInterior{}, err
	}
	interior.ContentHash = hash
	return interior, nil
}

// ComputeWorldgenBuildingInteriorHash produces the content hash stored with a
// materialized V2 floor. Database identity and timestamps are intentionally
// not inputs, so recovery can independently re-create and verify the plan.
func ComputeWorldgenBuildingInteriorHash(interior *GeneratedWorldgenBuildingInterior) (string, error) {
	if interior == nil || strings.TrimSpace(interior.BuildingCode) == "" ||
		interior.LayoutVersion != DefaultWorldgenInteriorVersion || len(interior.Cells) == 0 {
		return "", ErrInvalidWorldgenInput
	}
	raw, err := json.Marshal(struct {
		BuildingCode  string                          `json:"building_code"`
		FloorIndex    int32                           `json:"floor_index"`
		Z             int32                           `json:"z"`
		LayoutVersion string                          `json:"layout_version"`
		LayoutStyle   string                          `json:"layout_style"`
		Cells         []GeneratedWorldgenInteriorCell `json:"cells"`
	}{
		interior.BuildingCode,
		interior.FloorIndex,
		interior.Z,
		interior.LayoutVersion,
		interior.LayoutStyle,
		interior.Cells,
	})
	if err != nil {
		return "", fmt.Errorf("marshal worldgen building interior: %w", err)
	}
	sum := sha256.Sum256(raw)
	return fmt.Sprintf("%x", sum[:]), nil
}

func worldgenInteriorMask(
	binding WorldgenBinding,
	building GeneratedWorldgenBuilding,
	floorIndex int32,
) (map[buildingLayoutPoint]bool, buildingLayoutBounds, buildingLayoutPoint, error) {
	if binding.WorldSeed <= 0 || strings.TrimSpace(binding.GeneratorID) == "" ||
		strings.TrimSpace(binding.GeneratorVersion) == "" || !validSHA256Hex(binding.ProfileHash) ||
		strings.TrimSpace(building.Code) == "" || building.FloorCount <= 0 ||
		floorIndex < 0 || floorIndex >= building.FloorCount || len(building.Footprint) == 0 {
		return nil, buildingLayoutBounds{}, buildingLayoutPoint{}, ErrInvalidWorldgenInput
	}
	minimumX, maximumX := building.Footprint[0].X, building.Footprint[0].X
	minimumY, maximumY := building.Footprint[0].Y, building.Footprint[0].Y
	baseZ := building.Entrance.Z
	for _, point := range building.Footprint {
		if point.Z != baseZ {
			return nil, buildingLayoutBounds{}, buildingLayoutPoint{}, ErrInvalidWorldgenInput
		}
		if point.X < minimumX {
			minimumX = point.X
		}
		if point.X > maximumX {
			maximumX = point.X
		}
		if point.Y < minimumY {
			minimumY = point.Y
		}
		if point.Y > maximumY {
			maximumY = point.Y
		}
	}
	width, height := maximumX-minimumX+1, maximumY-minimumY+1
	if width < 3 || height < 3 || width > 256 || height > 256 {
		return nil, buildingLayoutBounds{}, buildingLayoutPoint{}, ErrInvalidWorldgenInput
	}
	mask := make(map[buildingLayoutPoint]bool, len(building.Footprint))
	for _, point := range building.Footprint {
		local := buildingLayoutPoint{x: point.X - minimumX, y: point.Y - minimumY}
		if mask[local] {
			return nil, buildingLayoutBounds{}, buildingLayoutPoint{}, ErrInvalidWorldgenInput
		}
		mask[local] = true
	}
	entrance := buildingLayoutPoint{x: building.Entrance.X - minimumX, y: building.Entrance.Y - minimumY}
	if !mask[entrance] {
		return nil, buildingLayoutBounds{}, buildingLayoutPoint{}, ErrInvalidWorldgenInput
	}
	return mask, buildingLayoutBounds{minX: minimumX, maxX: maximumX, minY: minimumY, maxY: maximumY}, entrance, nil
}

func worldgenInteriorSeed(binding WorldgenBinding, building GeneratedWorldgenBuilding, floorIndex int32) uint64 {
	payload := strings.Join([]string{
		DefaultWorldgenInteriorVersion,
		binding.GeneratorID,
		binding.GeneratorVersion,
		binding.ProfileHash,
		strconv.FormatInt(binding.WorldSeed, 10),
		building.Code,
		building.ArchetypeCode,
		building.LayoutStyle,
		strconv.FormatInt(int64(floorIndex), 10),
	}, "\x00")
	sum := sha256.Sum256([]byte(payload))
	return binary.BigEndian.Uint64(sum[:8])
}

func worldgenInteriorFrontage(
	entrance buildingLayoutPoint,
	bounds buildingLayoutBounds,
	seed uint64,
) buildingLayoutFrontage {
	distances := []struct {
		frontage buildingLayoutFrontage
		distance int64
	}{
		{buildingLayoutFrontageWest, entrance.x},
		{buildingLayoutFrontageNorth, entrance.y},
		{buildingLayoutFrontageEast, bounds.maxX - bounds.minX - entrance.x},
		{buildingLayoutFrontageSouth, bounds.maxY - bounds.minY - entrance.y},
	}
	minimum := distances[0].distance
	for _, candidate := range distances[1:] {
		if candidate.distance < minimum {
			minimum = candidate.distance
		}
	}
	candidates := make([]buildingLayoutFrontage, 0, 2)
	for _, candidate := range distances {
		if candidate.distance == minimum {
			candidates = append(candidates, candidate.frontage)
		}
	}
	return candidates[int(seed%uint64(len(candidates)))]
}

func worldgenInteriorVariation(style string, seed uint64) int {
	switch style {
	case "courtyard", "arcade":
		return 2
	case "rowhouse", "shopfront", "loading_depot":
		return 1
	case "walkup", "tower", "workshop":
		return 3
	default:
		return int(seed % 4)
	}
}

func worldgenInteriorStairPoint(
	mask map[buildingLayoutPoint]bool,
	width, height int,
	entrance buildingLayoutPoint,
	seed uint64,
) (buildingLayoutPoint, bool) {
	type candidate struct {
		point    buildingLayoutPoint
		distance int
	}
	candidates := make([]candidate, 0)
	centerX, centerY := width/2, height/2
	for point := range mask {
		if point == entrance {
			continue
		}
		interior := true
		for _, offset := range buildingLayoutCardinalOffsets {
			if !mask[buildingLayoutPoint{x: point.x + offset.x, y: point.y + offset.y}] {
				interior = false
				break
			}
		}
		if !interior {
			continue
		}
		distance := absWorldgenInteriorInt(int(point.x)-centerX) + absWorldgenInteriorInt(int(point.y)-centerY)
		candidates = append(candidates, candidate{point: point, distance: distance})
	}
	if len(candidates) == 0 {
		return buildingLayoutPoint{}, false
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].distance != candidates[j].distance {
			return candidates[i].distance < candidates[j].distance
		}
		if candidates[i].point.y != candidates[j].point.y {
			return candidates[i].point.y < candidates[j].point.y
		}
		return candidates[i].point.x < candidates[j].point.x
	})
	nearCenter := maxInt(1, (len(candidates)+2)/3)
	return candidates[int(seed%uint64(nearCenter))].point, true
}

func absWorldgenInteriorInt(value int) int {
	if value < 0 {
		return -value
	}
	return value
}
