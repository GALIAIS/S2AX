package cityspatial

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// DefaultBuildingLayoutVersion is deliberately independent from the immutable
// F7 land-foundation generator. It describes a deterministic local rendering
// and navigation projection derived from persisted building and portal facts.
// That lets existing worlds gain richer interiors without rewriting their
// baseline hashes.
const DefaultBuildingLayoutVersion = "1.1.0"

type BuildingLayoutCellKind string

const (
	BuildingLayoutCellWall      BuildingLayoutCellKind = "wall"
	BuildingLayoutCellWindow    BuildingLayoutCellKind = "window"
	BuildingLayoutCellFloor     BuildingLayoutCellKind = "floor"
	BuildingLayoutCellDoor      BuildingLayoutCellKind = "door"
	BuildingLayoutCellFurniture BuildingLayoutCellKind = "furniture"
)

type GeneratedBuildingLayoutCell struct {
	X       int64                  `json:"x"`
	Y       int64                  `json:"y"`
	Z       int32                  `json:"z"`
	Kind    BuildingLayoutCellKind `json:"kind"`
	Feature string                 `json:"feature,omitempty"`
}

type GeneratedBuildingLayout struct {
	BuildingCode  string                        `json:"building_code"`
	LayoutVersion string                        `json:"layout_version"`
	Archetype     string                        `json:"archetype"`
	Cells         []GeneratedBuildingLayoutCell `json:"cells"`
}

type buildingLayoutPoint struct {
	x int64
	y int64
	z int32
}

type buildingLayoutTile struct {
	kind    BuildingLayoutCellKind
	feature string
}

type buildingLayoutBounds struct {
	minX int64
	maxX int64
	minY int64
	maxY int64
}

type buildingLayoutFrontage uint8

const (
	buildingLayoutFrontageWest buildingLayoutFrontage = iota
	buildingLayoutFrontageNorth
	buildingLayoutFrontageEast
	buildingLayoutFrontageSouth
)

var buildingLayoutCardinalOffsets = [...]buildingLayoutPoint{
	{x: 0, y: -1},
	{x: 1, y: 0},
	{x: 0, y: 1},
	{x: -1, y: 0},
}

// GenerateBuildingLayouts derives all local building plans in a stable code
// order. It intentionally accepts persisted facts rather than any mutable
// process-global random state, so API rendering and navigation can agree.
func GenerateBuildingLayouts(
	buildings []GeneratedBuilding,
	portals []GeneratedBuildingPortal,
) ([]GeneratedBuildingLayout, error) {
	orderedBuildings := append([]GeneratedBuilding(nil), buildings...)
	sort.Slice(orderedBuildings, func(i, j int) bool {
		return orderedBuildings[i].Code < orderedBuildings[j].Code
	})
	portalsByBuilding := make(map[string][]GeneratedBuildingPortal, len(orderedBuildings))
	for _, portal := range portals {
		if strings.TrimSpace(portal.BuildingCode) == "" || portal.Status != "" && portal.Status != "active" {
			continue
		}
		portalsByBuilding[portal.BuildingCode] = append(portalsByBuilding[portal.BuildingCode], portal)
	}
	for buildingCode := range portalsByBuilding {
		sort.Slice(portalsByBuilding[buildingCode], func(i, j int) bool {
			return portalsByBuilding[buildingCode][i].Code < portalsByBuilding[buildingCode][j].Code
		})
	}

	result := make([]GeneratedBuildingLayout, 0, len(orderedBuildings))
	seen := make(map[string]struct{}, len(orderedBuildings))
	for _, building := range orderedBuildings {
		if _, duplicate := seen[building.Code]; duplicate {
			return nil, fmt.Errorf("%w: duplicate building layout code %q", ErrInvalidLandInput, building.Code)
		}
		seen[building.Code] = struct{}{}
		layout, err := GenerateBuildingLayout(building, portalsByBuilding[building.Code])
		if err != nil {
			return nil, err
		}
		result = append(result, layout)
	}
	return result, nil
}

// GenerateBuildingLayout projects one persisted building into an irregular,
// multi-room local blueprint. The layouts borrow the useful pattern of classic
// ASCII roguelike mapgen: choose a deterministic archetype, establish an
// outline, place interior partitions and furnishings, then anchor doors and
// stairs to authoritative portal facts. It does not import game content.
func GenerateBuildingLayout(
	building GeneratedBuilding,
	portals []GeneratedBuildingPortal,
) (GeneratedBuildingLayout, error) {
	bounds, err := buildingLayoutWorldBounds(building)
	if err != nil {
		return GeneratedBuildingLayout{}, err
	}
	seed := buildingLayoutSeed(building)
	variation := int(seed % 4)
	archetype := buildingLayoutArchetype(building.PrimaryUse, variation)
	frontage := buildingLayoutResolveFrontage(bounds, portals, seed)
	portalPoints := buildingLayoutPortalPoints(bounds, building, portals)
	width := int(bounds.maxX - bounds.minX + 1)
	height := int(bounds.maxY - bounds.minY + 1)
	if width <= 0 || height <= 0 {
		return GeneratedBuildingLayout{}, fmt.Errorf("%w: invalid layout dimensions for %q", ErrInvalidLandInput, building.Code)
	}

	layout := GeneratedBuildingLayout{
		BuildingCode: building.Code, LayoutVersion: DefaultBuildingLayoutVersion,
		Archetype: archetype, Cells: make([]GeneratedBuildingLayoutCell, 0, width*height*int(building.TopZ-building.BaseZ+1)),
	}
	for z := building.BaseZ; z <= building.TopZ; z++ {
		protected := portalPoints[z]
		mask := buildBuildingLayoutMask(width, height, building.PrimaryUse, variation, frontage, protected)
		tiles := classifyBuildingLayoutMask(mask, width, height, seed, z)
		applyBuildingLayoutPartition(tiles, width, height, building.PrimaryUse, variation, frontage, seed, z)
		applyBuildingLayoutPortalAnchors(tiles, protected)
		applyBuildingLayoutFurnishings(tiles, building.PrimaryUse, seed, z, protected)
		if !ensureBuildingLayoutConnectivity(tiles, protected) {
			return GeneratedBuildingLayout{}, fmt.Errorf("%w: disconnected generated layout for %q at z=%d", ErrInvalidLandInput, building.Code, z)
		}
		for point, tile := range tiles {
			layout.Cells = append(layout.Cells, GeneratedBuildingLayoutCell{
				X: point.x + bounds.minX, Y: point.y + bounds.minY, Z: z,
				Kind: tile.kind, Feature: tile.feature,
			})
		}
	}
	sort.Slice(layout.Cells, func(i, j int) bool {
		left, right := layout.Cells[i], layout.Cells[j]
		if left.Z != right.Z {
			return left.Z < right.Z
		}
		if left.Y != right.Y {
			return left.Y < right.Y
		}
		return left.X < right.X
	})
	return layout, nil
}

func buildingLayoutWorldBounds(building GeneratedBuilding) (buildingLayoutBounds, error) {
	if strings.TrimSpace(building.Code) == "" || building.BaseZ > building.TopZ ||
		!validLandRectangle(building.Footprint) {
		return buildingLayoutBounds{}, fmt.Errorf("%w: invalid building layout input", ErrInvalidLandInput)
	}
	minX := building.Footprint.ChunkX*DefaultChunkSize + int64(building.Footprint.LocalMinX)
	maxX := building.Footprint.ChunkX*DefaultChunkSize + int64(building.Footprint.LocalMaxX)
	minY := building.Footprint.ChunkY*DefaultChunkSize + int64(building.Footprint.LocalMinY)
	maxY := building.Footprint.ChunkY*DefaultChunkSize + int64(building.Footprint.LocalMaxY)
	if minX > maxX || minY > maxY {
		return buildingLayoutBounds{}, fmt.Errorf("%w: invalid building layout bounds", ErrInvalidLandInput)
	}
	return buildingLayoutBounds{minX: minX, maxX: maxX, minY: minY, maxY: maxY}, nil
}

func buildingLayoutSeed(building GeneratedBuilding) uint64 {
	payload := strings.Join([]string{
		DefaultBuildingLayoutVersion,
		building.Code,
		string(building.PrimaryUse),
		strconv.FormatInt(building.Footprint.ChunkX, 10),
		strconv.FormatInt(building.Footprint.ChunkY, 10),
		strconv.FormatInt(int64(building.Footprint.LocalMinX), 10),
		strconv.FormatInt(int64(building.Footprint.LocalMinY), 10),
		strconv.FormatInt(int64(building.Footprint.LocalMaxX), 10),
		strconv.FormatInt(int64(building.Footprint.LocalMaxY), 10),
		strconv.FormatInt(int64(building.BaseZ), 10),
		strconv.FormatInt(int64(building.TopZ), 10),
	}, "\x00")
	hash := sha256.Sum256([]byte(payload))
	return binary.BigEndian.Uint64(hash[:8])
}

func buildingLayoutArchetype(use LandUse, variation int) string {
	switch use {
	case LandUseResidential:
		return []string{
			"residential.courtyard_house",
			"residential.rowhouse",
			"residential.l_house",
			"residential.walkup",
		}[variation%4]
	case LandUseCommercial:
		return []string{
			"commercial.shopfront",
			"commercial.corner_market",
			"commercial.arcade",
			"commercial.office_lobby",
		}[variation%4]
	case LandUseIndustrial:
		return []string{
			"industrial.warehouse",
			"industrial.workshop",
			"industrial.loading_depot",
			"industrial.foundry",
		}[variation%4]
	default:
		return "mixed.generic"
	}
}

func buildingLayoutResolveFrontage(
	bounds buildingLayoutBounds,
	portals []GeneratedBuildingPortal,
	seed uint64,
) buildingLayoutFrontage {
	for _, portal := range portals {
		if portal.Status != "" && portal.Status != "active" || portal.PortalType != "entrance" {
			continue
		}
		switch {
		case portal.ToX == bounds.minX && portal.FromX < bounds.minX:
			return buildingLayoutFrontageWest
		case portal.ToX == bounds.maxX && portal.FromX > bounds.maxX:
			return buildingLayoutFrontageEast
		case portal.ToY == bounds.minY && portal.FromY < bounds.minY:
			return buildingLayoutFrontageNorth
		case portal.ToY == bounds.maxY && portal.FromY > bounds.maxY:
			return buildingLayoutFrontageSouth
		}
	}
	return buildingLayoutFrontage(seed % 4)
}

func buildingLayoutPortalPoints(
	bounds buildingLayoutBounds,
	building GeneratedBuilding,
	portals []GeneratedBuildingPortal,
) map[int32]map[buildingLayoutPoint]string {
	result := make(map[int32]map[buildingLayoutPoint]string, int(building.TopZ-building.BaseZ+1))
	for _, portal := range portals {
		if portal.BuildingCode != building.Code || portal.Status != "" && portal.Status != "active" {
			continue
		}
		for _, point := range []buildingLayoutPoint{
			{x: portal.FromX, y: portal.FromY, z: portal.FromZ},
			{x: portal.ToX, y: portal.ToY, z: portal.ToZ},
		} {
			if point.x < bounds.minX || point.x > bounds.maxX || point.y < bounds.minY || point.y > bounds.maxY ||
				point.z < building.BaseZ || point.z > building.TopZ {
				continue
			}
			// The enclosing map is already partitioned by z. Keeping the
			// relative point planar prevents a stair on another floor from
			// becoming a second, disconnected tile in this floor's plan.
			relative := buildingLayoutPoint{x: point.x - bounds.minX, y: point.y - bounds.minY}
			if result[point.z] == nil {
				result[point.z] = make(map[buildingLayoutPoint]string)
			}
			current := result[point.z][relative]
			if current == "" || portal.PortalType == "entrance" {
				result[point.z][relative] = portal.PortalType
			}
		}
	}
	return result
}

func buildBuildingLayoutMask(
	width, height int,
	use LandUse,
	variation int,
	frontage buildingLayoutFrontage,
	protected map[buildingLayoutPoint]string,
) map[buildingLayoutPoint]bool {
	mask := make(map[buildingLayoutPoint]bool, width*height)
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			mask[buildingLayoutPoint{x: int64(x), y: int64(y)}] = true
		}
	}
	if width < 5 || height < 5 {
		return mask
	}

	cutSize := 1
	if width >= 6 && height >= 6 {
		cutSize = 2
	}
	corner := variation % 4
	switch use {
	case LandUseResidential:
		switch variation {
		case 0:
			buildingLayoutCutCorner(mask, width, height, corner, cutSize, protected)
		case 1:
			buildingLayoutCutCorner(mask, width, height, corner, 1, protected)
			buildingLayoutCutFrontSetback(mask, width, height, frontage, 1, protected)
		case 2:
			if width >= 8 && height >= 8 {
				buildingLayoutCutCourtyard(mask, width, height, protected)
			} else {
				buildingLayoutCutCorner(mask, width, height, corner, 1, protected)
				buildingLayoutCutFrontSetback(mask, width, height, buildingLayoutOpposite(frontage), 1, protected)
			}
		default:
			buildingLayoutCutCorner(mask, width, height, corner, cutSize, protected)
			buildingLayoutCutCorner(mask, width, height, (corner+2)%4, 1, protected)
		}
	case LandUseCommercial:
		switch variation {
		case 0, 1:
			buildingLayoutCutCorner(mask, width, height, corner, 1, protected)
		case 2:
			buildingLayoutCutFrontSetback(mask, width, height, frontage, cutSize, protected)
		default:
			buildingLayoutCutCorner(mask, width, height, corner, cutSize, protected)
		}
	case LandUseIndustrial:
		switch variation {
		case 0:
			buildingLayoutCutCorner(mask, width, height, corner, 1, protected)
		case 1:
			buildingLayoutCutLoadingBay(mask, width, height, frontage, cutSize, protected)
		case 2:
			buildingLayoutCutCorner(mask, width, height, corner, cutSize, protected)
			buildingLayoutCutLoadingBay(mask, width, height, buildingLayoutOpposite(frontage), 1, protected)
		default:
			buildingLayoutCutFrontSetback(mask, width, height, buildingLayoutOpposite(frontage), 1, protected)
		}
	default:
		buildingLayoutCutCorner(mask, width, height, corner, 1, protected)
	}
	return mask
}

func buildingLayoutCutCorner(
	mask map[buildingLayoutPoint]bool,
	width, height, corner, size int,
	protected map[buildingLayoutPoint]string,
) {
	if size <= 0 {
		return
	}
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			targetX, targetY := x, y
			switch corner % 4 {
			case 1:
				targetX = width - 1 - x
			case 2:
				targetX, targetY = width-1-x, height-1-y
			case 3:
				targetY = height - 1 - y
			}
			point := buildingLayoutPoint{x: int64(targetX), y: int64(targetY)}
			if _, reserved := protected[point]; !reserved {
				delete(mask, point)
			}
		}
	}
}

func buildingLayoutCutFrontSetback(
	mask map[buildingLayoutPoint]bool,
	width, height int,
	frontage buildingLayoutFrontage,
	depth int,
	protected map[buildingLayoutPoint]string,
) {
	if depth <= 0 {
		return
	}
	spanStart, spanEnd := 1, maxInt(1, width-2)
	if frontage == buildingLayoutFrontageWest || frontage == buildingLayoutFrontageEast {
		spanEnd = maxInt(1, height-2)
	}
	for offset := 0; offset < depth; offset++ {
		for span := spanStart; span <= spanEnd; span++ {
			x, y := buildingLayoutFrontCoordinate(width, height, frontage, offset, span)
			point := buildingLayoutPoint{x: int64(x), y: int64(y)}
			if _, reserved := protected[point]; !reserved &&
				!buildingLayoutEntranceLane(protected, point, frontage) {
				delete(mask, point)
			}
		}
	}
}

func buildingLayoutEntranceLane(
	protected map[buildingLayoutPoint]string,
	point buildingLayoutPoint,
	frontage buildingLayoutFrontage,
) bool {
	for protectedPoint, portalType := range protected {
		if portalType != "entrance" {
			continue
		}
		switch frontage {
		case buildingLayoutFrontageWest, buildingLayoutFrontageEast:
			if point.y == protectedPoint.y {
				return true
			}
		case buildingLayoutFrontageNorth, buildingLayoutFrontageSouth:
			if point.x == protectedPoint.x {
				return true
			}
		}
	}
	return false
}

func buildingLayoutCutLoadingBay(
	mask map[buildingLayoutPoint]bool,
	width, height int,
	frontage buildingLayoutFrontage,
	size int,
	protected map[buildingLayoutPoint]string,
) {
	if size <= 0 {
		return
	}
	start := 1
	if frontage == buildingLayoutFrontageWest || frontage == buildingLayoutFrontageEast {
		start = maxInt(1, (height-size)/2)
	} else {
		start = maxInt(1, (width-size)/2)
	}
	for offset := 0; offset < size; offset++ {
		x, y := buildingLayoutFrontCoordinate(width, height, frontage, 0, start+offset)
		point := buildingLayoutPoint{x: int64(x), y: int64(y)}
		if _, reserved := protected[point]; !reserved {
			delete(mask, point)
		}
	}
}

func buildingLayoutCutCourtyard(
	mask map[buildingLayoutPoint]bool,
	width, height int,
	protected map[buildingLayoutPoint]string,
) {
	if width < 6 || height < 6 {
		return
	}
	centerX, centerY := width/2, height/2
	for y := centerY - 1; y <= centerY; y++ {
		for x := centerX - 1; x <= centerX; x++ {
			point := buildingLayoutPoint{x: int64(x), y: int64(y)}
			if _, reserved := protected[point]; !reserved {
				delete(mask, point)
			}
		}
	}
}

func buildingLayoutFrontCoordinate(
	width, height int,
	frontage buildingLayoutFrontage,
	depth, span int,
) (int, int) {
	switch frontage {
	case buildingLayoutFrontageWest:
		return depth, span
	case buildingLayoutFrontageNorth:
		return span, depth
	case buildingLayoutFrontageEast:
		return width - 1 - depth, span
	default:
		return span, height - 1 - depth
	}
}

func buildingLayoutOpposite(frontage buildingLayoutFrontage) buildingLayoutFrontage {
	return (frontage + 2) % 4
}

func classifyBuildingLayoutMask(
	mask map[buildingLayoutPoint]bool,
	width, height int,
	seed uint64,
	z int32,
) map[buildingLayoutPoint]buildingLayoutTile {
	tiles := make(map[buildingLayoutPoint]buildingLayoutTile, len(mask))
	for point := range mask {
		boundary := false
		for _, offset := range buildingLayoutCardinalOffsets {
			neighbor := buildingLayoutPoint{x: point.x + offset.x, y: point.y + offset.y}
			if neighbor.x < 0 || neighbor.x >= int64(width) || neighbor.y < 0 || neighbor.y >= int64(height) || !mask[neighbor] {
				boundary = true
				break
			}
		}
		kind := BuildingLayoutCellFloor
		if boundary {
			kind = BuildingLayoutCellWall
			if (uint64(point.x*31+point.y*17+int64(z)*13)^seed)%5 == 0 {
				kind = BuildingLayoutCellWindow
			}
		}
		tiles[point] = buildingLayoutTile{kind: kind}
	}
	return tiles
}

func applyBuildingLayoutPartition(
	tiles map[buildingLayoutPoint]buildingLayoutTile,
	width, height int,
	use LandUse,
	variation int,
	frontage buildingLayoutFrontage,
	seed uint64,
	z int32,
) {
	if width < 5 || height < 5 {
		return
	}
	vertical := false
	switch use {
	case LandUseResidential:
		vertical = frontage == buildingLayoutFrontageWest || frontage == buildingLayoutFrontageEast
	case LandUseCommercial:
		vertical = frontage == buildingLayoutFrontageNorth || frontage == buildingLayoutFrontageSouth
	case LandUseIndustrial:
		vertical = variation%2 == 0
	default:
		vertical = variation%2 == 0
	}
	candidates := make([]buildingLayoutPoint, 0)
	if vertical {
		column := int64(width / 2)
		for y := 1; y < height-1; y++ {
			point := buildingLayoutPoint{x: column, y: int64(y)}
			if tile, ok := tiles[point]; ok && tile.kind == BuildingLayoutCellFloor {
				candidates = append(candidates, point)
			}
		}
	} else {
		row := int64(height / 2)
		for x := 1; x < width-1; x++ {
			point := buildingLayoutPoint{x: int64(x), y: row}
			if tile, ok := tiles[point]; ok && tile.kind == BuildingLayoutCellFloor {
				candidates = append(candidates, point)
			}
		}
	}
	if len(candidates) < 2 {
		return
	}
	doorIndex := int((seed + uint64(z)*7) % uint64(len(candidates)))
	for index, point := range candidates {
		tile := tiles[point]
		if index == doorIndex {
			tile.kind, tile.feature = BuildingLayoutCellDoor, ""
		} else {
			tile.kind, tile.feature = BuildingLayoutCellWall, ""
		}
		tiles[point] = tile
	}
}

func applyBuildingLayoutPortalAnchors(
	tiles map[buildingLayoutPoint]buildingLayoutTile,
	protected map[buildingLayoutPoint]string,
) {
	for point, portalType := range protected {
		tile, exists := tiles[point]
		if !exists {
			tile = buildingLayoutTile{kind: BuildingLayoutCellFloor}
		}
		if portalType == "entrance" {
			tile.kind, tile.feature = BuildingLayoutCellDoor, ""
		} else {
			tile.kind, tile.feature = BuildingLayoutCellFloor, "stairs"
		}
		tiles[point] = tile
	}
}

func applyBuildingLayoutFurnishings(
	tiles map[buildingLayoutPoint]buildingLayoutTile,
	use LandUse,
	seed uint64,
	z int32,
	protected map[buildingLayoutPoint]string,
) {
	features := buildingLayoutFeatures(use)
	if len(features) == 0 {
		return
	}
	candidates := make([]buildingLayoutPoint, 0)
	for point, tile := range tiles {
		if tile.kind != BuildingLayoutCellFloor {
			continue
		}
		if _, reserved := protected[point]; reserved {
			continue
		}
		candidates = append(candidates, point)
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].y != candidates[j].y {
			return candidates[i].y < candidates[j].y
		}
		return candidates[i].x < candidates[j].x
	})
	// A fixed one-of-each feature list made a 20×20 interior look as empty as
	// a 6×6 room. Furnishing density must scale with usable floor area while
	// keeping a deterministic upper bound. Furniture is currently traversable,
	// so this preserves the navigation topology established before this step.
	limit := minInt(
		len(candidates),
		maxInt(len(features), (len(candidates)+9)/10),
	)
	for index := 0; index < limit; index++ {
		candidateIndex := buildingLayoutFurnishingCandidateIndex(seed, z, index, len(candidates))
		point := candidates[candidateIndex]
		for {
			if tiles[point].kind == BuildingLayoutCellFloor {
				break
			}
			candidateIndex = (candidateIndex + 1) % len(candidates)
			point = candidates[candidateIndex]
		}
		tiles[point] = buildingLayoutTile{
			kind:    BuildingLayoutCellFurniture,
			feature: features[(index+int(seed%uint64(len(features))))%len(features)],
		}
	}
}

func buildingLayoutFurnishingCandidateIndex(seed uint64, z int32, index, candidateCount int) int {
	value := seed ^ uint64(uint32(z))*0x9e3779b97f4a7c15 ^ uint64(index+1)*0xbf58476d1ce4e5b9
	value ^= value >> 30
	value *= 0xbf58476d1ce4e5b9
	value ^= value >> 27
	value *= 0x94d049bb133111eb
	value ^= value >> 31
	return int(value % uint64(candidateCount))
}

func buildingLayoutFeatures(use LandUse) []string {
	switch use {
	case LandUseResidential:
		return []string{"bed", "table", "chair"}
	case LandUseCommercial:
		return []string{"counter", "shelf", "table"}
	case LandUseIndustrial:
		return []string{"machine", "crate", "shelf"}
	default:
		return []string{"table", "crate"}
	}
}

func buildingLayoutPassableCellsConnected(
	tiles map[buildingLayoutPoint]buildingLayoutTile,
	protected map[buildingLayoutPoint]string,
) bool {
	visited, passable := buildingLayoutReachablePassableCells(tiles, protected)
	return len(visited) == len(passable)
}

// ensureBuildingLayoutConnectivity makes partition placement a constrained
// operation: a partition can only remain closed if it does not isolate a
// walkable room. When a generated divider does isolate a room, one divider
// segment becomes an interior door. This keeps the same deterministic outline
// while preserving a valid navigation graph.
func ensureBuildingLayoutConnectivity(
	tiles map[buildingLayoutPoint]buildingLayoutTile,
	protected map[buildingLayoutPoint]string,
) bool {
	for repairs := 0; repairs < len(tiles); repairs++ {
		visited, passable := buildingLayoutReachablePassableCells(tiles, protected)
		if len(passable) == 0 {
			return false
		}
		if len(visited) == len(passable) {
			return true
		}
		repaired := false
		for _, point := range sortedBuildingLayoutTilePoints(tiles) {
			tile := tiles[point]
			if tile.kind != BuildingLayoutCellWall && tile.kind != BuildingLayoutCellWindow {
				continue
			}
			touchesReachable, touchesIsolated := false, false
			for _, offset := range buildingLayoutCardinalOffsets {
				neighbor := buildingLayoutPoint{x: point.x + offset.x, y: point.y + offset.y}
				if _, isPassable := passable[neighbor]; !isPassable {
					continue
				}
				if _, isReachable := visited[neighbor]; isReachable {
					touchesReachable = true
				} else {
					touchesIsolated = true
				}
			}
			if touchesReachable && touchesIsolated {
				tiles[point] = buildingLayoutTile{kind: BuildingLayoutCellDoor}
				repaired = true
				break
			}
		}
		if !repaired {
			return false
		}
	}
	return false
}

func buildingLayoutReachablePassableCells(
	tiles map[buildingLayoutPoint]buildingLayoutTile,
	protected map[buildingLayoutPoint]string,
) (map[buildingLayoutPoint]struct{}, map[buildingLayoutPoint]struct{}) {
	passable := make(map[buildingLayoutPoint]struct{})
	for point, tile := range tiles {
		if buildingLayoutCellPassable(tile.kind) {
			passable[point] = struct{}{}
		}
	}
	if len(passable) == 0 {
		return map[buildingLayoutPoint]struct{}{}, passable
	}
	var root buildingLayoutPoint
	rootFound := false
	for _, point := range sortedBuildingLayoutPortalPoints(protected) {
		if _, ok := passable[point]; ok {
			root, rootFound = point, true
			break
		}
	}
	if !rootFound {
		for _, point := range sortedBuildingLayoutPassablePoints(passable) {
			root, rootFound = point, true
			break
		}
	}
	visited := map[buildingLayoutPoint]struct{}{root: {}}
	queue := []buildingLayoutPoint{root}
	for len(queue) > 0 {
		point := queue[0]
		queue = queue[1:]
		for _, offset := range buildingLayoutCardinalOffsets {
			next := buildingLayoutPoint{x: point.x + offset.x, y: point.y + offset.y}
			if _, exists := passable[next]; !exists {
				continue
			}
			if _, seen := visited[next]; seen {
				continue
			}
			visited[next] = struct{}{}
			queue = append(queue, next)
		}
	}
	return visited, passable
}

func sortedBuildingLayoutTilePoints(
	tiles map[buildingLayoutPoint]buildingLayoutTile,
) []buildingLayoutPoint {
	points := make([]buildingLayoutPoint, 0, len(tiles))
	for point := range tiles {
		points = append(points, point)
	}
	sortBuildingLayoutPoints(points)
	return points
}

func sortedBuildingLayoutPortalPoints(
	portals map[buildingLayoutPoint]string,
) []buildingLayoutPoint {
	points := make([]buildingLayoutPoint, 0, len(portals))
	for point := range portals {
		points = append(points, point)
	}
	sortBuildingLayoutPoints(points)
	return points
}

func sortedBuildingLayoutPassablePoints(
	passable map[buildingLayoutPoint]struct{},
) []buildingLayoutPoint {
	points := make([]buildingLayoutPoint, 0, len(passable))
	for point := range passable {
		points = append(points, point)
	}
	sortBuildingLayoutPoints(points)
	return points
}

func sortBuildingLayoutPoints(points []buildingLayoutPoint) {
	sort.Slice(points, func(i, j int) bool {
		if points[i].y != points[j].y {
			return points[i].y < points[j].y
		}
		return points[i].x < points[j].x
	})
}

func BuildingLayoutCellPassable(kind BuildingLayoutCellKind) bool {
	return buildingLayoutCellPassable(kind)
}

func buildingLayoutCellPassable(kind BuildingLayoutCellKind) bool {
	return kind == BuildingLayoutCellFloor || kind == BuildingLayoutCellDoor || kind == BuildingLayoutCellFurniture
}

func minInt(left, right int) int {
	if left < right {
		return left
	}
	return right
}

func maxInt(left, right int) int {
	if left > right {
		return left
	}
	return right
}
