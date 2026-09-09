package render

// GeometryType describes the shape of a renderable entity.
type GeometryType uint8

const (
	GeometryPolygon GeometryType = iota
	GeometryPolyline
	GeometrySprite
)

// Geometry holds raw vertex data for a renderable shape.
// For polygons and polylines, Coords is interleaved x,y int32 pairs.
// The int32 format preserves precision for large world-space coordinates
// (e.g. degrees × scale).
type Geometry struct {
	Type   GeometryType
	Coords []int32
	Closed bool // polylines only: whether to close the loop
}

// Appearance holds the visual style of a renderable entity.
// A negative RGB value means "no fill" or "no stroke" respectively.
type Appearance struct {
	FillR, FillG, FillB, FillA         float32
	StrokeR, StrokeG, StrokeB, StrokeA float32
	StrokeWidthPx                      float32
	MinSegmentPx                       float32
}

// HasFill reports whether this appearance has a fill colour.
func (a Appearance) HasFill() bool { return a.FillA >= 0 }

// HasStroke reports whether this appearance has a stroke colour.
func (a Appearance) HasStroke() bool { return a.StrokeA >= 0 }

// Renderable is a marker component that ties an entity to its geometry and
// appearance for the render system. GeometryIndex indexes into the scene's
// geometry slice; Rank controls LOD; Order controls draw ordering within a
// pass.
type Renderable struct {
	GeometryIndex int
	Rank          int
	Order         float32
}

// Bounds is an axis-aligned bounding box in int32 world-space coordinates.
type Bounds struct {
	MinX, MinY, MaxX, MaxY int32
}

// Clamp255 clamps a float32 in [0,1] to a uint32 in [0,255].
func Clamp255(v float32) uint32 {
	if v <= 0 {
		return 0
	}
	if v >= 1 {
		return 255
	}
	return uint32(v * 255)
}
