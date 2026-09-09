package maprender

import (
	"github.com/abdallah-elbeheiry/AqwaborEngine/ecs"
)

// MapScene is the plain-data component carrying per-scene render settings. The
// heavy resources, the mesh and the strokes and the renderer itself, hold
// pointers and so cannot be components; they live in a Scenes table beside the
// world and are reached by entity.
type MapScene struct {
	ClearR, ClearG, ClearB, ClearA float32
	WorldScale                     float32
	Active                         uint8 // 1 = draw this scene
}

// Scenes binds entities to the renderers that draw them.
//
// It used to be a package-level map, which made every world in a process share
// one table, never released an entry for a destroyed entity, and could not be
// touched from two goroutines. One of these belongs to whoever owns the world.
type Scenes struct {
	comp ecs.Comp[MapScene]
	byE  map[ecs.Entity]*Renderer
}

// RegisterECS registers MapScene and returns the table used to bind and draw.
func RegisterECS(w *ecs.World) (*Scenes, error) {
	c, err := ecs.Register[MapScene](w)
	if err != nil {
		return nil, err
	}
	return &Scenes{comp: c, byE: make(map[ecs.Entity]*Renderer)}, nil
}

// MustRegisterECS is RegisterECS, panicking on error.
func MustRegisterECS(w *ecs.World) *Scenes {
	s, err := RegisterECS(w)
	if err != nil {
		panic("maprender.RegisterECS: " + err.Error())
	}
	return s
}

// Component returns the handle for the MapScene component.
func (s *Scenes) Component() ecs.Comp[MapScene] { return s.comp }

// Bind associates an entity with a renderer so Draw can find it.
func (s *Scenes) Bind(e ecs.Entity, r *Renderer) { s.byE[e] = r }

// Unbind drops the association. Call it when the entity is destroyed, since
// nothing else releases the renderer this table holds.
func (s *Scenes) Unbind(e ecs.Entity) { delete(s.byE, e) }

// Draw draws the scene attached to e. The caller owns the render pass, and the
// renderer must have been given its render.Renderer beforehand.
func (s *Scenes) Draw(e ecs.Entity) {
	scene, ok := s.comp.Get(e)
	if !ok || scene.Active == 0 {
		return
	}
	r, ok := s.byE[e]
	if !ok || r == nil {
		return
	}
	r.Draw()
}

// DrawAll draws every awake, active scene bound in this table.
func (s *Scenes) DrawAll() {
	s.comp.Each(func(e ecs.Entity, scene *MapScene) {
		if scene.Active == 0 {
			return
		}
		if r, ok := s.byE[e]; ok && r != nil {
			r.Draw()
		}
	})
}
