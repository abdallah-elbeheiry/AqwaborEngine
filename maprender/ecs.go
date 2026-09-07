package maprender

import (
	"github.com/abdallah-elbeheiry/AqwaborEngine/ecs"
)

// MapScene is a PoD component that carries per-scene render metadata.
// Heavy resources (mesh, strokes, renderer) live in the Bind side table,
// not in the component.
type MapScene struct {
	ClearR, ClearG, ClearB, ClearA float32
	WorldScale                     float32
	Active                         uint8 // 1 = draw this scene
}

// RegisterECS registers the MapScene component type.
func RegisterECS(w *ecs.World) error {
	return ecs.Register[MapScene](w)
}

// MustRegisterECS is like RegisterECS but panics on error.
func MustRegisterECS(w *ecs.World) {
	if err := RegisterECS(w); err != nil {
		panic("maprender.RegisterECS: " + err.Error())
	}
}

// bindTable maps entities to their GPU renderer resources.
// This is a side table, not an ECS component — it holds non-PoD pointers.
var bindTable = map[ecs.Entity]*Renderer{}

// Bind associates an entity with a renderer so DrawECS can draw it.
func Bind(e ecs.Entity, r *Renderer) {
	bindTable[e] = r
}

// Unbind removes an entity from the bind table.
func Unbind(e ecs.Entity) {
	delete(bindTable, e)
}

// DrawECS draws the map scene attached to e (fills + strokes).
// The caller must own the render pass (Begin/End). The renderer must
// have had SetRenderer called before this.
func DrawECS(w *ecs.World, e ecs.Entity) {
	scene, ok := ecs.Get[MapScene](w, e)
	if !ok || scene.Active == 0 {
		return
	}
	r, ok := bindTable[e]
	if !ok || r == nil {
		return
	}
	r.Draw()
}
