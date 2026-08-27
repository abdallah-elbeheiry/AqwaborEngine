package maprender

// Ring triangulation for map fills.
//
// The map arrives as closed rings, not triangles, so something has to turn a
// coastline into an interior before it can reach a triangle-list pipeline.
// This is ear clipping: repeatedly cut off a corner that contains no other
// vertex, until three vertices remain.
//
// Coordinates are converted to float64 degrees here. The int32 storage exists
// so the simulation stays deterministic across machines; a triangulation is a
// rendering artifact that never feeds back into the simulation, so floats are
// safe on this side of the boundary and keep the geometric predicates well
// within precision.

// ringClipper holds one ring as a doubly linked list, so removing a clipped
// vertex costs two pointer writes and invalidates only its two neighbours.
// A slice with copy() would re-sweep the whole ring after every clip, which on
// the 22,908-vertex ring in this dataset is the difference between a usable
// load and a stalled one.
type ringClipper struct {
	xs, ys []float64
	prev   []int32
	next   []int32
	reflex []bool
	// relaxed lets a vertex lying exactly on an ear's edge pass instead of
	// blocking it. Coordinates are quantised to 1.11 cm, so exact collinearity
	// is common enough that the strict test stalls on real rings.
	relaxed bool
	ccw     bool
}

// triangulateRing returns triangle corner indices local to the ring, three per
// triangle. Input is interleaved x,y in scaled degrees; scale converts them to
// degrees. Never returns nil for a ring of three or more vertices: a ring that
// defeats ear clipping falls back to a fan.
func triangulateRing(coords []int32, scale float64) []int32 {
	n := len(coords) / 2

	// Every closed ring in this dataset repeats its first vertex at the end,
	// because the GeoJSON convention survived the packing step. A repeated
	// vertex is a zero-area corner that ear clipping cannot cut, so it is
	// dropped here rather than being special-cased throughout.
	for n >= 2 && coords[0] == coords[(n-1)*2] && coords[1] == coords[(n-1)*2+1] {
		n--
	}
	if n < 3 {
		return nil
	}
	coords = coords[:n*2]

	c := &ringClipper{
		xs:     make([]float64, n),
		ys:     make([]float64, n),
		prev:   make([]int32, n),
		next:   make([]int32, n),
		reflex: make([]bool, n),
	}

	// Ear clipping assumes counter-clockwise input. Rings arrive in either
	// winding, so the link order absorbs the flip and the coordinates stay put.
	c.ccw = signedArea2(coords, scale) >= 0
	for i := range n {
		c.xs[i] = float64(coords[i*2]) / scale
		c.ys[i] = float64(coords[i*2+1]) / scale
	}
	c.reset(n)

	if out := c.clip(n); out != nil {
		return out
	}
	// A ring that defeats the strict test gets one relaxed pass before the fan.
	c.relaxed = true
	c.reset(n)
	if out := c.clip(n); out != nil {
		return out
	}
	// Self-intersecting or otherwise not simple. Natural Earth carries a few.
	// A fan is wrong in detail but keeps the landmass on screen instead of
	// dropping it.
	return fanFallback(n)
}

// clip runs ear clipping to completion, or returns nil if it stalls.
func (c *ringClipper) clip(n int) []int32 {
	out := make([]int32, 0, (n-2)*3)
	remaining := n
	cur := int32(0)
	// Every vertex may be rejected once before an ear is found, so a full lap
	// without a clip means no ear exists.
	stall := 0
	for remaining > 3 {
		p, nx := c.prev[cur], c.next[cur]
		if c.isEar(p, cur, nx, remaining) {
			out = append(out, p, cur, nx)
			c.next[p] = nx
			c.prev[nx] = p
			c.updateReflex(p)
			c.updateReflex(nx)
			remaining--
			stall = 0
			cur = nx
			continue
		}
		cur = nx
		stall++
		if stall > remaining {
			return nil
		}
	}
	out = append(out, c.prev[cur], cur, c.next[cur])
	return out
}

// isEar reports whether the corner at b is a convex corner containing no other
// vertex of the ring.
// reset relinks the ring into its original order, so a failed pass can be
// retried without rebuilding the coordinates.
func (c *ringClipper) reset(n int) {
	for i := range n {
		if c.ccw {
			c.next[i] = int32((i + 1) % n)
			c.prev[i] = int32((i - 1 + n) % n)
		} else {
			c.next[i] = int32((i - 1 + n) % n)
			c.prev[i] = int32((i + 1) % n)
		}
	}
	for i := range n {
		c.updateReflex(int32(i))
	}
}

func (c *ringClipper) isEar(a, b, d int32, remaining int) bool {
	if c.reflex[b] {
		return false
	}
	// Only a reflex vertex can sit inside an ear, and only one inside the
	// ear's bounding box. Both tests are here because the box test is four
	// comparisons against three cross products.
	minX := min(c.xs[a], c.xs[b], c.xs[d])
	maxX := max(c.xs[a], c.xs[b], c.xs[d])
	minY := min(c.ys[a], c.ys[b], c.ys[d])
	maxY := max(c.ys[a], c.ys[b], c.ys[d])

	j := c.next[d]
	for range remaining - 3 {
		if c.reflex[j] {
			x, y := c.xs[j], c.ys[j]
			if x >= minX && x <= maxX && y >= minY && y <= maxY &&
				c.pointInTri(j, a, b, d) {
				return false
			}
		}
		j = c.next[j]
	}
	return true
}

func (c *ringClipper) updateReflex(i int32) {
	c.reflex[i] = c.orient(c.prev[i], i, c.next[i]) <= 0
}

func (c *ringClipper) orient(a, b, d int32) float64 {
	return (c.xs[b]-c.xs[a])*(c.ys[d]-c.ys[a]) - (c.ys[b]-c.ys[a])*(c.xs[d]-c.xs[a])
}

func (c *ringClipper) pointInTri(p, a, b, d int32) bool {
	if c.relaxed {
		// Strictly inside, so a vertex sitting exactly on an edge no longer
		// blocks the ear.
		return c.orient(a, b, p) > 0 && c.orient(b, d, p) > 0 && c.orient(d, a, p) > 0
	}
	return c.orient(a, b, p) >= 0 && c.orient(b, d, p) >= 0 && c.orient(d, a, p) >= 0
}

func fanFallback(n int) []int32 {
	out := make([]int32, 0, (n-2)*3)
	for i := 1; i < n-1; i++ {
		out = append(out, 0, int32(i), int32(i+1))
	}
	return out
}

// signedArea2 is twice the signed area in degrees squared; positive is
// counter-clockwise.
func signedArea2(coords []int32, scale float64) float64 {
	s := 0.0
	n := len(coords) / 2
	for i := range n {
		j := (i + 1) % n
		s += float64(coords[i*2])/scale*(float64(coords[j*2+1])/scale) -
			float64(coords[j*2])/scale*(float64(coords[i*2+1])/scale)
	}
	return s
}
