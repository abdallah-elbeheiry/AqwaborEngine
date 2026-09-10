package maprender

import (
	"math"
	"sort"
	"unsafe"

	"github.com/abdallah-elbeheiry/AqwaborEngine/render"
	"github.com/gogpu/gputypes"
	"github.com/gogpu/wgpu"
)

// MaxRank is the highest rank the data uses. The filter clamps to 0..12, so a
// pass keeps one cumulative count per rank in that range.
const MaxRank = 12

// metresPerDegree is one degree of latitude at the equator, and is what turns
// camera zoom into the ground resolution the rank filter is written against.
const metresPerDegree = 110540

// PassRange records the vertex range for one DrawOrder pass in the shared
// buffer, and how much of that range each level of detail needs.
//
// Geometry within a pass is emitted in ascending rank order, so everything up to
// a given rank is a prefix of the range. Drawing a level of detail is then a
// smaller Count rather than a per-frame cull: ByRank[r] is how many vertices to
// draw when the maximum rank is r.
type PassRange struct {
	Offset uint32
	Count  uint32
	ByRank [MaxRank + 1]uint32
}

// CountFor is how many vertices to draw at the given maximum rank.
func (p PassRange) CountFor(maxRank int) uint32 {
	if maxRank < 0 {
		maxRank = 0
	}
	if maxRank > MaxRank {
		maxRank = MaxRank
	}
	return p.ByRank[maxRank]
}

// RankForZoom is the maximum rank worth drawing at a given zoom, which is the
// filter the map data documents: the more ground a pixel covers, the fewer
// minor rivers and lakes are worth drawing.
func RankForZoom(zoom float32) int {
	if zoom <= 0 {
		return MaxRank
	}
	mpx := metresPerDegree / float64(zoom)
	v := 16 - 3*math.Log10(math.Max(mpx, 1e-6))
	return int(math.Max(0, math.Min(MaxRank, v)))
}

// MapMesh holds pre-built GPU geometry for the entire map.
type MapMesh struct {
	Buffer *wgpu.Buffer
	Passes []PassRange
	Total  uint32
}

// MapMeshConfig controls how the map mesh is built.
type MapMeshConfig struct {
	StrokeWidthPx float32
	MinSegmentPx  float32
	RefZoom       float32
	Scale         float32
}

// FillGeometry is one triangulated polygon ready for GPU upload.
type FillGeometry struct {
	Coords []int32
	Tris   []int32
	Rank   int
	Fill   render.Color
}

// StrokeGeometry is one polyline ready for GPU upload.
type StrokeGeometry struct {
	Coords []int32
	Closed bool
	Rank   int
	Color  [4]float32
}

// DrawPassSpec describes one ordered group of geometries to draw together.
type DrawPassSpec struct {
	GeomIndices []int32
	FillColor   *render.Color
	StrokeColor *render.Color
	RankFilter  bool
}

// BuildMapMesh triangulates fills and uploads them to a single GPU vertex
// buffer. It operates on generic FillGeometry and DrawPassSpec rather than
// map-specific types.
func BuildMapMesh(
	dev *wgpu.Device,
	fillGeoms []FillGeometry,
	passes []DrawPassSpec,
	cfg MapMeshConfig,
) *MapMesh {
	vertices := make([]MapVertex, 0, 1<<20)

	var passRanges []PassRange

	for _, pass := range passes {
		start := uint32(len(vertices))

		// Emit in ascending rank, so the geometry for any level of detail is a
		// prefix of this pass's range.
		ids := sortedFillByRank(fillGeoms, pass.GeomIndices, pass.RankFilter)

		var byRank [MaxRank + 1]uint32
		filled := 0
		for _, geomIdx := range ids {
			if int(geomIdx) >= len(fillGeoms) {
				continue
			}
			fg := &fillGeoms[geomIdx]
			n := len(fg.Coords) / 2
			if n < 2 {
				continue
			}

			if pass.FillColor != nil && fg.Tris != nil {
				emitFill(&vertices, fg.Coords, fg.Tris, *pass.FillColor)
			}

			if pass.RankFilter {
				r := clampRank(fg.Rank)
				for filled <= r {
					byRank[filled] = uint32(len(vertices)) - start
					filled++
				}
				byRank[r] = uint32(len(vertices)) - start
			}
		}

		count := uint32(len(vertices)) - start
		if !pass.RankFilter {
			for i := range byRank {
				byRank[i] = count
			}
		} else {
			for i := filled; i <= MaxRank; i++ {
				byRank[i] = count
			}
		}

		passRanges = append(passRanges, PassRange{Offset: start, Count: count, ByRank: byRank})
	}

	total := uint32(len(vertices))
	if total == 0 {
		return &MapMesh{}
	}

	bufBytes := int(total) * 12
	buf, err := dev.CreateBuffer(&wgpu.BufferDescriptor{
		Label:            "map mesh",
		Size:             uint64(bufBytes),
		Usage:            gputypes.BufferUsageVertex | gputypes.BufferUsageCopyDst,
		MappedAtCreation: true,
	})
	if err != nil {
		panic(err)
	}
	mr, err := buf.MappedRange(0, uint64(bufBytes))
	if err != nil {
		panic(err)
	}
	copy(mr.Bytes(), unsafe.Slice((*byte)(unsafe.Pointer(&vertices[0])), bufBytes))
	mr.Release()
	buf.Unmap()

	return &MapMesh{Buffer: buf, Passes: passRanges, Total: total}
}

func emitFill(out *[]MapVertex, coords []int32, tris []int32, c render.Color) {
	cr, cg, cb, ca := quantizeColor(c)
	n := len(coords) / 2
	for _, idx := range tris {
		if int(idx) >= n {
			continue
		}
		*out = append(*out, MapVertex{
			PosX: coords[idx*2],
			PosY: coords[idx*2+1],
			R:    cr, G: cg, B: cb, A: ca,
		})
	}
}

func quantizeColor(c render.Color) (cr, cg, cb, ca uint8) {
	cr = uint8(render.Clamp255(c.R))
	cg = uint8(render.Clamp255(c.G))
	cb = uint8(render.Clamp255(c.B))
	ca = uint8(render.Clamp255(c.A))
	return
}

func clampRank(r int) int {
	if r < 0 {
		return 0
	}
	if r > MaxRank {
		return MaxRank
	}
	return r
}

// sortedFillByRank orders a pass's geometry indices so that everything at or
// below a rank comes first. Sorting is stable, so geometry of equal rank keeps
// the order the data gave it.
func sortedFillByRank(fillGeoms []FillGeometry, ids []int32, rankFilter bool) []int32 {
	if !rankFilter || len(ids) < 2 {
		return ids
	}
	out := make([]int32, len(ids))
	copy(out, ids)
	sort.SliceStable(out, func(i, j int) bool {
		ri, rj := 0, 0
		if int(out[i]) < len(fillGeoms) {
			ri = fillGeoms[out[i]].Rank
		}
		if int(out[j]) < len(fillGeoms) {
			rj = fillGeoms[out[j]].Rank
		}
		return ri < rj
	})
	return out
}
