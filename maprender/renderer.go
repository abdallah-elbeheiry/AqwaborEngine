package maprender

import (
	"github.com/abdallah-elbeheiry/AqwaborEngine/camera"
	"github.com/abdallah-elbeheiry/AqwaborEngine/mapdata"
	"github.com/abdallah-elbeheiry/AqwaborEngine/window"
	"github.com/gogpu/gogpu"
	"github.com/gogpu/ui/geometry"
)

type Renderer struct {
	world    *mapdata.World
	cam      *camera.Camera
	viewport geometry.Size
	vertices []window.Vertex
	win      *window.Window
}

func NewRenderer(world *mapdata.World, cam *camera.Camera, win *window.Window) *Renderer {
	return &Renderer{
		world:    world,
		cam:      cam,
		win:      win,
		vertices: make([]window.Vertex, 0, 8192),
	}
}

func (r *Renderer) SetViewport(vp geometry.Size) {
	r.viewport = vp
}

func (r *Renderer) SetCamera(cam *camera.Camera) {
	r.cam = cam
}

func (r *Renderer) Draw(dc *gogpu.Context) error {
	if r.world == nil || r.cam == nil {
		return nil
	}

	r.vertices = r.vertices[:0]

	viewMinLon, viewMinLat, viewMaxLon, viewMaxLat := r.worldViewAABB()

	for _, pass := range r.world.DrawOrder {
		layer := &r.world.Layers[pass.LayerIndex]

		for _, geomID := range layer.GeomIDs {
			if !r.cullGeom(geomID, viewMinLon, viewMinLat, viewMaxLon, viewMaxLat) {
				continue
			}

			if pass.FillColor != nil && layer.Kind == mapdata.KindRing {
				r.drawRingFill(geomID, *pass.FillColor)
			}
			if pass.StrokeColor != nil {
				if layer.Kind == mapdata.KindRing {
					r.drawRingStroke(geomID, *pass.StrokeColor)
				} else if layer.Kind == mapdata.KindLine {
					r.drawLine(geomID, *pass.StrokeColor)
				}
			}
		}

		if len(r.vertices) > 0 {
			if err := r.win.Draw(dc, r.vertices); err != nil {
				return err
			}
			r.vertices = r.vertices[:0]
		}
	}

	return nil
}

func (r *Renderer) worldViewAABB() (minLon, minLat, maxLon, maxLat float32) {
	vp := r.viewport
	tl := r.cam.LocalToWorld(geometry.Pt(0, 0), vp)
	br := r.cam.LocalToWorld(geometry.Pt(vp.Width, vp.Height), vp)

	minLon = min(tl.X, br.X)
	maxLon = max(tl.X, br.X)
	// Flip Y: camera Y increases up, but we negate lat for rendering
	minLat = -max(tl.Y, br.Y)
	maxLat = -min(tl.Y, br.Y)

	return
}

func (r *Renderer) cullGeom(geomID int32, minLon, minLat, maxLon, maxLat float32) bool {
	scale := float32(r.world.Scale)
	gMinLon := float32(r.world.MinX[geomID]) / scale
	gMaxLon := float32(r.world.MaxX[geomID]) / scale
	gMinLat := float32(r.world.MinY[geomID]) / scale
	gMaxLat := float32(r.world.MaxY[geomID]) / scale

	if gMaxLon < minLon || gMinLon > maxLon || gMaxLat < minLat || gMinLat > maxLat {
		return false
	}
	return true
}

func (r *Renderer) drawRingFill(geomID int32, color mapdata.Color) {
	start := r.world.GeomStart[geomID]
	n := r.world.GeomN[geomID]

	r.ensureCapacity(int(n))
	vpW := r.viewport.Width
	vpH := r.viewport.Height
	for i := range n {
		idx := start + i*2
		lon := float32(r.world.Coords[idx]) / float32(r.world.Scale)
		lat := float32(r.world.Coords[idx+1]) / float32(r.world.Scale)
		local := r.cam.WorldToLocal(geometry.Pt(lon, -lat), r.viewport)
		r.vertices = append(r.vertices, window.Vertex{
			X: local.X/vpW*2 - 1, Y: 1 - local.Y/vpH*2,
			R: color.R, G: color.G, B: color.B, A: color.A,
		})
	}
}

func (r *Renderer) drawRingStroke(geomID int32, color mapdata.Color) {
	start := r.world.GeomStart[geomID]
	n := r.world.GeomN[geomID]

	r.ensureCapacity(int(n))
	vpW := r.viewport.Width
	vpH := r.viewport.Height
	for i := range n {
		idx := start + i*2
		lon := float32(r.world.Coords[idx]) / float32(r.world.Scale)
		lat := float32(r.world.Coords[idx+1]) / float32(r.world.Scale)
		local := r.cam.WorldToLocal(geometry.Pt(lon, -lat), r.viewport)
		r.vertices = append(r.vertices, window.Vertex{
			X: local.X/vpW*2 - 1, Y: 1 - local.Y/vpH*2,
			R: color.R, G: color.G, B: color.B, A: color.A,
		})
	}
}

func (r *Renderer) drawLine(geomID int32, color mapdata.Color) {
	r.drawRingStroke(geomID, color)
}

func (r *Renderer) ensureCapacity(n int) {
	if cap(r.vertices)-len(r.vertices) < n {
		newCap := max(cap(r.vertices)*2, len(r.vertices)+n)
		newVerts := make([]window.Vertex, len(r.vertices), newCap)
		copy(newVerts, r.vertices)
		r.vertices = newVerts
	}
}
