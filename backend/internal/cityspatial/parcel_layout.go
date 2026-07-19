package cityspatial

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"sort"
	"strings"
)

// DefaultParcelLayoutVersion describes deterministic, non-blocking site
// details around a building. It is intentionally a presentation layer: terrain
// collision remains authoritative until a future world-generator version
// persists site topology as a physical fact.
const DefaultParcelLayoutVersion = "1.0.0"

type ParcelLayoutCellKind string

const (
	ParcelLayoutCellPath     ParcelLayoutCellKind = "path"
	ParcelLayoutCellGarden   ParcelLayoutCellKind = "garden"
	ParcelLayoutCellTree     ParcelLayoutCellKind = "tree"
	ParcelLayoutCellSidewalk ParcelLayoutCellKind = "sidewalk"
	ParcelLayoutCellParking  ParcelLayoutCellKind = "parking"
	ParcelLayoutCellLoading  ParcelLayoutCellKind = "loading"
)

type GeneratedParcelLayoutCell struct {
	X    int64                `json:"x"`
	Y    int64                `json:"y"`
	Z    int32                `json:"z"`
	Kind ParcelLayoutCellKind `json:"kind"`
}

type GeneratedParcelLayout struct {
	ParcelCode    string                      `json:"parcel_code"`
	LayoutVersion string                      `json:"layout_version"`
	Style         string                      `json:"style"`
	Cells         []GeneratedParcelLayoutCell `json:"cells"`
}

// GenerateParcelLayouts derives paths and local site detail from the selected
// land facts. Building layout cells always win: this function only emits cells
// outside their structural footprint.
func GenerateParcelLayouts(
	parcels []GeneratedParcel,
	buildings []GeneratedBuilding,
	buildingLayouts []GeneratedBuildingLayout,
	portals []GeneratedBuildingPortal,
	z int32,
) ([]GeneratedParcelLayout, error) {
	orderedParcels := append([]GeneratedParcel(nil), parcels...)
	sort.Slice(orderedParcels, func(i, j int) bool {
		return orderedParcels[i].Code < orderedParcels[j].Code
	})

	buildingByParcel := make(map[string]GeneratedBuilding, len(buildings))
	for _, building := range buildings {
		if strings.TrimSpace(building.ParcelCode) == "" {
			continue
		}
		if _, duplicate := buildingByParcel[building.ParcelCode]; duplicate {
			return nil, fmt.Errorf("%w: duplicate building parcel %q", ErrInvalidLandInput, building.ParcelCode)
		}
		buildingByParcel[building.ParcelCode] = building
	}
	layoutByBuilding := make(map[string]GeneratedBuildingLayout, len(buildingLayouts))
	for _, layout := range buildingLayouts {
		if strings.TrimSpace(layout.BuildingCode) != "" {
			layoutByBuilding[layout.BuildingCode] = layout
		}
	}
	portalsByBuilding := make(map[string][]GeneratedBuildingPortal, len(buildings))
	for _, portal := range portals {
		if portal.Status != "" && portal.Status != "active" || strings.TrimSpace(portal.BuildingCode) == "" {
			continue
		}
		portalsByBuilding[portal.BuildingCode] = append(portalsByBuilding[portal.BuildingCode], portal)
	}
	for buildingCode := range portalsByBuilding {
		sort.Slice(portalsByBuilding[buildingCode], func(i, j int) bool {
			return portalsByBuilding[buildingCode][i].Code < portalsByBuilding[buildingCode][j].Code
		})
	}

	result := make([]GeneratedParcelLayout, 0, len(orderedParcels))
	for _, parcel := range orderedParcels {
		if parcel.Geometry.Z != z {
			continue
		}
		building := buildingByParcel[parcel.Code]
		layout, err := GenerateParcelLayout(
			parcel,
			building,
			layoutByBuilding[building.Code],
			portalsByBuilding[building.Code],
			z,
		)
		if err != nil {
			return nil, err
		}
		if len(layout.Cells) > 0 {
			result = append(result, layout)
		}
	}
	return result, nil
}

func GenerateParcelLayout(
	parcel GeneratedParcel,
	building GeneratedBuilding,
	buildingLayout GeneratedBuildingLayout,
	portals []GeneratedBuildingPortal,
	z int32,
) (GeneratedParcelLayout, error) {
	bounds, err := parcelLayoutWorldBounds(parcel.Geometry)
	if err != nil {
		return GeneratedParcelLayout{}, err
	}
	cells := make(map[buildingLayoutPoint]ParcelLayoutCellKind)
	occupied := make(map[buildingLayoutPoint]struct{})
	for _, cell := range buildingLayout.Cells {
		if cell.Z != z || cell.X < bounds.minX || cell.X > bounds.maxX || cell.Y < bounds.minY || cell.Y > bounds.maxY {
			continue
		}
		occupied[buildingLayoutPoint{x: cell.X, y: cell.Y}] = struct{}{}
	}
	if len(occupied) == 0 && building.Code != "" && building.BaseZ <= z && z <= building.TopZ {
		buildingBounds, boundsErr := buildingLayoutWorldBounds(building)
		if boundsErr != nil {
			return GeneratedParcelLayout{}, boundsErr
		}
		for y := maxInt64(bounds.minY, buildingBounds.minY); y <= minInt64(bounds.maxY, buildingBounds.maxY); y++ {
			for x := maxInt64(bounds.minX, buildingBounds.minX); x <= minInt64(bounds.maxX, buildingBounds.maxX); x++ {
				occupied[buildingLayoutPoint{x: x, y: y}] = struct{}{}
			}
		}
	}
	seed := parcelLayoutSeed(parcel)
	entry, hasEntry := parcelLayoutEntrance(bounds, building, portals, z)
	if hasEntry {
		parcelLayoutAccessPath(cells, occupied, bounds, entry, building)
	}

	candidates := parcelLayoutCandidates(bounds, occupied, cells)
	style := parcelLayoutStyle(LandUse(parcel.ZoneCode), seed)
	switch LandUse(parcel.ZoneCode) {
	case LandUseResidential:
		parcelLayoutPlace(cells, candidates, seed, 4+int(seed%4), ParcelLayoutCellGarden)
		parcelLayoutPlace(cells, candidates, seed>>9, 1+int(seed%3), ParcelLayoutCellTree)
	case LandUseCommercial:
		parcelLayoutPlace(cells, candidates, seed, 3+int(seed%3), ParcelLayoutCellSidewalk)
		parcelLayoutPlace(cells, candidates, seed>>11, 2+int(seed%4), ParcelLayoutCellParking)
	case LandUseIndustrial:
		parcelLayoutPlace(cells, candidates, seed, 2+int(seed%3), ParcelLayoutCellLoading)
		parcelLayoutPlace(cells, candidates, seed>>13, 2+int(seed%4), ParcelLayoutCellParking)
	default:
		parcelLayoutPlace(cells, candidates, seed, 2, ParcelLayoutCellSidewalk)
	}

	layout := GeneratedParcelLayout{
		ParcelCode: parcel.Code, LayoutVersion: DefaultParcelLayoutVersion,
		Style: style, Cells: make([]GeneratedParcelLayoutCell, 0, len(cells)),
	}
	for point, kind := range cells {
		layout.Cells = append(layout.Cells, GeneratedParcelLayoutCell{X: point.x, Y: point.y, Z: z, Kind: kind})
	}
	sort.Slice(layout.Cells, func(i, j int) bool {
		if layout.Cells[i].Y != layout.Cells[j].Y {
			return layout.Cells[i].Y < layout.Cells[j].Y
		}
		return layout.Cells[i].X < layout.Cells[j].X
	})
	return layout, nil
}

func parcelLayoutWorldBounds(rectangle LandRectangle) (buildingLayoutBounds, error) {
	if !validLandRectangle(rectangle) {
		return buildingLayoutBounds{}, fmt.Errorf("%w: invalid parcel layout geometry", ErrInvalidLandInput)
	}
	return buildingLayoutBounds{
		minX: rectangle.ChunkX*DefaultChunkSize + int64(rectangle.LocalMinX),
		maxX: rectangle.ChunkX*DefaultChunkSize + int64(rectangle.LocalMaxX),
		minY: rectangle.ChunkY*DefaultChunkSize + int64(rectangle.LocalMinY),
		maxY: rectangle.ChunkY*DefaultChunkSize + int64(rectangle.LocalMaxY),
	}, nil
}

func parcelLayoutSeed(parcel GeneratedParcel) uint64 {
	hash := sha256.Sum256([]byte(DefaultParcelLayoutVersion + "|" + parcel.Code + "|" + parcel.ZoneCode))
	return binary.BigEndian.Uint64(hash[:8])
}

func parcelLayoutStyle(use LandUse, seed uint64) string {
	switch use {
	case LandUseResidential:
		return []string{"residential.garden", "residential.courtyard", "residential.drive"}[seed%3]
	case LandUseCommercial:
		return []string{"commercial.frontage", "commercial.plaza", "commercial.parking"}[seed%3]
	case LandUseIndustrial:
		return []string{"industrial.loading", "industrial.yard", "industrial.depot"}[seed%3]
	default:
		return "mixed.site"
	}
}

func parcelLayoutEntrance(
	bounds buildingLayoutBounds,
	building GeneratedBuilding,
	portals []GeneratedBuildingPortal,
	z int32,
) (buildingLayoutPoint, bool) {
	if building.Code == "" {
		return buildingLayoutPoint{}, false
	}
	for _, portal := range portals {
		if portal.PortalType != "entrance" {
			continue
		}
		for _, point := range []buildingLayoutPoint{
			{x: portal.FromX, y: portal.FromY, z: portal.FromZ},
			{x: portal.ToX, y: portal.ToY, z: portal.ToZ},
		} {
			if point.z == z && point.x >= bounds.minX && point.x <= bounds.maxX &&
				point.y >= bounds.minY && point.y <= bounds.maxY {
				return point, true
			}
		}
	}
	return buildingLayoutPoint{}, false
}

func parcelLayoutAccessPath(
	cells map[buildingLayoutPoint]ParcelLayoutCellKind,
	occupied map[buildingLayoutPoint]struct{},
	bounds buildingLayoutBounds,
	entry buildingLayoutPoint,
	building GeneratedBuilding,
) {
	buildingBounds, err := buildingLayoutWorldBounds(building)
	if err != nil {
		return
	}
	directionX, directionY := int64(0), int64(0)
	switch {
	case entry.x <= buildingBounds.minX:
		directionX = -1
	case entry.x >= buildingBounds.maxX:
		directionX = 1
	case entry.y <= buildingBounds.minY:
		directionY = -1
	default:
		directionY = 1
	}
	for point := entry; point.x >= bounds.minX && point.x <= bounds.maxX && point.y >= bounds.minY && point.y <= bounds.maxY; point.x, point.y = point.x+directionX, point.y+directionY {
		if _, isBuilding := occupied[point]; !isBuilding {
			cells[point] = ParcelLayoutCellPath
		}
	}
}

func parcelLayoutCandidates(
	bounds buildingLayoutBounds,
	occupied map[buildingLayoutPoint]struct{},
	cells map[buildingLayoutPoint]ParcelLayoutCellKind,
) []buildingLayoutPoint {
	result := make([]buildingLayoutPoint, 0)
	for y := bounds.minY; y <= bounds.maxY; y++ {
		for x := bounds.minX; x <= bounds.maxX; x++ {
			point := buildingLayoutPoint{x: x, y: y}
			if _, isBuilding := occupied[point]; isBuilding {
				continue
			}
			if _, reserved := cells[point]; reserved {
				continue
			}
			result = append(result, point)
		}
	}
	return result
}

func parcelLayoutPlace(
	cells map[buildingLayoutPoint]ParcelLayoutCellKind,
	candidates []buildingLayoutPoint,
	seed uint64,
	count int,
	kind ParcelLayoutCellKind,
) {
	if count <= 0 || len(candidates) == 0 {
		return
	}
	for index := 0; index < count; index++ {
		candidateIndex := int((seed >> uint(index*7)) % uint64(len(candidates)))
		for checked := 0; checked < len(candidates); checked++ {
			point := candidates[candidateIndex]
			if _, used := cells[point]; !used {
				cells[point] = kind
				break
			}
			candidateIndex = (candidateIndex + 1) % len(candidates)
		}
	}
}

func minInt64(left, right int64) int64 {
	if left < right {
		return left
	}
	return right
}

func maxInt64(left, right int64) int64 {
	if left > right {
		return left
	}
	return right
}
