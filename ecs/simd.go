package ecs

import (
	"github.com/viterin/vek"
	"github.com/viterin/vek/vek32"
)

// FieldGetter extracts a *float64 field from a component passed as any.
// The any holds a properly typed *T pointer; the getter type-asserts.
type FieldGetter func(c any) *float64

// FieldGetter32 extracts a *float32 field from a component passed as any.
type FieldGetter32 func(c any) *float32

// tryGetter safely calls the getter, returning nil if the type assertion panics.
func tryGetter(getter FieldGetter, data any) (result *float64) {
	defer func() {
		if r := recover(); r != nil {
			result = nil
		}
	}()
	return getter(data)
}

func tryGetter32(getter FieldGetter32, data any) (result *float32) {
	defer func() {
		if r := recover(); r != nil {
			result = nil
		}
	}()
	return getter(data)
}

// componentData returns the wrapped component data for the given entity and component ID.
func (g *Group) componentData(e Entity, cid ComponentID) (any, bool) {
	meta := g.w.entities.meta(e)
	if meta == nil || !meta.alive {
		return nil, false
	}
	h, ok := meta.components[cid]
	if !ok {
		return nil, false
	}
	inst, ok := g.w.pool.get(h)
	if !ok {
		return nil, false
	}
	info := g.w.registry.componentInfoFor(cid)
	if info == nil {
		return nil, false
	}
	return info.wrap(inst.data), true
}

// entityCids returns the component IDs to iterate for a given entity.
// For signature-based groups, returns the declared ids.
// For entity-based groups, returns all of the entity's component IDs.
func (g *Group) entityCids(e Entity) []ComponentID {
	if g.ids != nil {
		return g.ids
	}
	meta := g.w.entities.meta(e)
	if meta == nil {
		return nil
	}
	cids := make([]ComponentID, 0, len(meta.components))
	for cid := range meta.components {
		cids = append(cids, cid)
	}
	return cids
}

func (g *Group) Float64s(getter FieldGetter) []float64 {
	g.resolve()
	result := make([]float64, 0, len(g.members))
	for _, e := range g.members {
		for _, cid := range g.entityCids(e) {
			data, ok := g.componentData(e, cid)
			if !ok {
				continue
			}
			if ptr := tryGetter(getter, data); ptr != nil {
				result = append(result, *ptr)
				break
			}
		}
	}
	return result
}

func (g *Group) Float32s(getter FieldGetter32) []float32 {
	g.resolve()
	result := make([]float32, 0, len(g.members))
	for _, e := range g.members {
		for _, cid := range g.entityCids(e) {
			data, ok := g.componentData(e, cid)
			if !ok {
				continue
			}
			if ptr := tryGetter32(getter, data); ptr != nil {
				result = append(result, *ptr)
				break
			}
		}
	}
	return result
}

func (g *Group) scatter64(values []float64, setter func(c any, v float64)) {
	g.resolve()
	idx := 0
	for _, e := range g.members {
		if idx >= len(values) {
			break
		}
		for _, cid := range g.entityCids(e) {
			data, ok := g.componentData(e, cid)
			if !ok {
				continue
			}
			wrote := false
			func() {
				defer func() { recover() }()
				setter(data, values[idx])
				wrote = true
			}()
			if wrote {
				idx++
				break
			}
		}
	}
}

func (g *Group) scatter32(values []float32, setter func(c any, v float32)) {
	g.resolve()
	idx := 0
	for _, e := range g.members {
		if idx >= len(values) {
			break
		}
		for _, cid := range g.entityCids(e) {
			data, ok := g.componentData(e, cid)
			if !ok {
				continue
			}
			wrote := false
			func() {
				defer func() { recover() }()
				setter(data, values[idx])
				wrote = true
			}()
			if wrote {
				idx++
				break
			}
		}
	}
}

// --- Bulk float64 ops (SIMD-accelerated via vek) ---

func (g *Group) AddVec(a, b FieldGetter) {
	xs := g.Float64s(a)
	ys := g.Float64s(b)
	vek.Add_Inplace(xs, ys)
	g.scatter64(xs, func(c any, v float64) { *a(c) = v })
}

func (g *Group) SubVec(a, b FieldGetter) {
	xs := g.Float64s(a)
	ys := g.Float64s(b)
	vek.Sub_Inplace(xs, ys)
	g.scatter64(xs, func(c any, v float64) { *a(c) = v })
}

func (g *Group) MulVec(a, b FieldGetter) {
	xs := g.Float64s(a)
	ys := g.Float64s(b)
	vek.Mul_Inplace(xs, ys)
	g.scatter64(xs, func(c any, v float64) { *a(c) = v })
}

func (g *Group) DivVec(a, b FieldGetter) {
	xs := g.Float64s(a)
	ys := g.Float64s(b)
	vek.Div_Inplace(xs, ys)
	g.scatter64(xs, func(c any, v float64) { *a(c) = v })
}

func (g *Group) AddNumber(a FieldGetter, n float64) {
	xs := g.Float64s(a)
	vek.AddNumber_Inplace(xs, n)
	g.scatter64(xs, func(c any, v float64) { *a(c) = v })
}

func (g *Group) SubNumber(a FieldGetter, n float64) {
	xs := g.Float64s(a)
	vek.SubNumber_Inplace(xs, n)
	g.scatter64(xs, func(c any, v float64) { *a(c) = v })
}

func (g *Group) MulNumber(a FieldGetter, n float64) {
	xs := g.Float64s(a)
	vek.MulNumber_Inplace(xs, n)
	g.scatter64(xs, func(c any, v float64) { *a(c) = v })
}

func (g *Group) DivNumber(a FieldGetter, n float64) {
	xs := g.Float64s(a)
	vek.DivNumber_Inplace(xs, n)
	g.scatter64(xs, func(c any, v float64) { *a(c) = v })
}

// --- Bulk float32 ops (SIMD-accelerated via vek32) ---

func (g *Group) AddVec32(a, b FieldGetter32) {
	xs := g.Float32s(a)
	ys := g.Float32s(b)
	vek32.Add_Inplace(xs, ys)
	g.scatter32(xs, func(c any, v float32) { *a(c) = v })
}

func (g *Group) SubVec32(a, b FieldGetter32) {
	xs := g.Float32s(a)
	ys := g.Float32s(b)
	vek32.Sub_Inplace(xs, ys)
	g.scatter32(xs, func(c any, v float32) { *a(c) = v })
}

func (g *Group) MulVec32(a, b FieldGetter32) {
	xs := g.Float32s(a)
	ys := g.Float32s(b)
	vek32.Mul_Inplace(xs, ys)
	g.scatter32(xs, func(c any, v float32) { *a(c) = v })
}

func (g *Group) DivVec32(a, b FieldGetter32) {
	xs := g.Float32s(a)
	ys := g.Float32s(b)
	vek32.Div_Inplace(xs, ys)
	g.scatter32(xs, func(c any, v float32) { *a(c) = v })
}

func (g *Group) AddNumber32(a FieldGetter32, n float32) {
	xs := g.Float32s(a)
	vek32.AddNumber_Inplace(xs, n)
	g.scatter32(xs, func(c any, v float32) { *a(c) = v })
}

func (g *Group) SubNumber32(a FieldGetter32, n float32) {
	xs := g.Float32s(a)
	vek32.SubNumber_Inplace(xs, n)
	g.scatter32(xs, func(c any, v float32) { *a(c) = v })
}

func (g *Group) MulNumber32(a FieldGetter32, n float32) {
	xs := g.Float32s(a)
	vek32.MulNumber_Inplace(xs, n)
	g.scatter32(xs, func(c any, v float32) { *a(c) = v })
}

func (g *Group) DivNumber32(a FieldGetter32, n float32) {
	xs := g.Float32s(a)
	vek32.DivNumber_Inplace(xs, n)
	g.scatter32(xs, func(c any, v float32) { *a(c) = v })
}
