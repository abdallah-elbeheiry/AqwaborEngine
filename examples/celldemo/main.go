// Command celldemo draws the compact cell layer: a grid where a cell's colour
// is a palette index rather than an RGBA value.
//
// It is the 16-byte instance path, against 64 for a sprite. A cell carries a
// position and an index; the colour comes from a ramp uploaded once and the
// size from a uniform, because a grid shares one size.
//
//	go run ./examples/celldemo
//	go run ./examples/celldemo -side 800   # 640,000 cells
//
// Keys: E/Q or =/- zoom, WASD or arrows pan, R re-rolls the palette so you can
// see that a recolour is an index change rather than a rewrite.
package main

import (
	"flag"
	"math"
	"time"

	"github.com/abdallah-elbeheiry/AqwaborEngine/camera"
	"github.com/abdallah-elbeheiry/AqwaborEngine/ecs"
	"github.com/abdallah-elbeheiry/AqwaborEngine/input"
	gogpuinput "github.com/abdallah-elbeheiry/AqwaborEngine/input/backend/gogpu"
	"github.com/abdallah-elbeheiry/AqwaborEngine/logx"
	"github.com/abdallah-elbeheiry/AqwaborEngine/render"
	"github.com/abdallah-elbeheiry/AqwaborEngine/window"

	"github.com/gogpu/gogpu"
)

// materials is how many colours the grid draws with.
const materials = 16

func main() {
	logx.Init(logx.WithColor(true), logx.WithLevel(logx.InfoLevel))
	side := flag.Int("side", 400, "cells a side; the grid is this squared")
	flag.Parse()

	win, err := window.NewWindow(window.WindowConfig{
		Title:     "Aqwabor - Cells",
		W:         1280,
		H:         720,
		Resizable: true,
	})
	if err != nil {
		logx.Fatalf("window: %v", err)
	}
	defer win.Close()

	w := ecs.NewWorld()
	camComp := camera.MustRegisterECS(w)

	vpW, vpH := float32(1280), float32(720)
	camE := w.Create()
	camComp.Set(camE, camera.Camera{Zoom: 2, MinZoom: 0.05, MaxZoom: 60, Active: 1})
	camComp.Wake(camE)
	cam, _ := camComp.Get(camE)
	cam.X, cam.Y = float32(*side)/2, float32(*side)/2

	mgr := input.NewManager(gogpuinput.NewBackend(win.App()))
	var reroll bool
	bind := func(name string, key input.Key, fn func(*camera.Camera)) {
		a := mgr.Action(name)
		mgr.BindKey(a, key)
		a.OnPressed(func(input.Context) {
			if c, ok := camComp.Get(camE); ok {
				fn(c)
			}
		})
	}
	bind("zoom_in", input.KeyE, func(c *camera.Camera) { c.Zoom *= 1.1; camera.ClampZoom(c) })
	bind("zoom_out", input.KeyQ, func(c *camera.Camera) { c.Zoom /= 1.1; camera.ClampZoom(c) })
	bind("zoom_in_ansi", input.KeyEqual, func(c *camera.Camera) { c.Zoom *= 1.1; camera.ClampZoom(c) })
	bind("zoom_out_ansi", input.KeyMinus, func(c *camera.Camera) { c.Zoom /= 1.1; camera.ClampZoom(c) })
	pan := func(dx, dy float32) func(*camera.Camera) {
		return func(c *camera.Camera) { c.X += dx; c.Y += dy }
	}
	bind("left", input.KeyA, pan(-8, 0))
	bind("right", input.KeyD, pan(8, 0))
	bind("up", input.KeyW, pan(0, -8))
	bind("down", input.KeyS, pan(0, 8))
	bind("left_arrow", input.KeyLeft, pan(-8, 0))
	bind("right_arrow", input.KeyRight, pan(8, 0))
	bind("up_arrow", input.KeyUp, pan(0, -8))
	bind("down_arrow", input.KeyDown, pan(0, 8))

	rerollAction := mgr.Action("reroll")
	mgr.BindKey(rerollAction, input.KeyR)
	rerollAction.OnPressed(func(input.Context) { reroll = true })

	win.OnUpdate(func(dt float64) { mgr.Update(dt) })

	var gfx *render.GPU
	var cells *render.SubcellBuffer
	var ramp render.RampTable
	var phase float32
	var frame int
	var reported time.Time

	err = win.Run(func(dc *gogpu.Context) {
		if w, h := dc.Size(); w > 0 && h > 0 {
			vpW, vpH = float32(w), float32(h)
		}
		frame++

		if gfx == nil {
			gfx = render.New(win.DeviceProvider())

			// One cell per world unit, so the camera's zoom is pixels a cell
			// and nothing converts between two scales.
			gfx.SetCellSize(1, 1)
			setRamp(gfx, &ramp, 0)

			// The grid is written once. From here the only thing that changes
			// is the ramp, which is what makes a recolour cheap: a moving light
			// becomes a change of index per cell, or of the table itself.
			cells = gfx.Subcells(*side * *side)
			grid := make([]render.SubcellInstance, 0, *side**side)
			for y := range *side {
				for x := range *side {
					grid = append(grid, render.SubcellInstance{
						X: float32(x) + 0.5, Y: float32(y) + 0.5,
						Palette: render.PaletteOf((x/8 + y/8) % materials),
					})
				}
			}
			cells.WriteAll(grid)
			logx.Info("grid built", "cells", len(grid), "bytes", len(grid)*16)
		}

		if reroll {
			phase += 1
			setRamp(gfx, &ramp, phase)
			reroll = false
		}

		c, _ := camComp.Get(camE)
		if err := gfx.Begin(dc, render.Clear{R: 0.03, G: 0.03, B: 0.05, A: 1}); err != nil {
			return
		}
		gfx.SetCamera(camera.ViewProj(*c, vpW, vpH), vpW, vpH)
		gfx.DrawSubcells(cells)
		gfx.End()

		if time.Since(reported) > time.Second {
			s := gfx.Stats()
			logx.Info("frame", "cells", cells.Count(),
				"draws", s.DrawCalls, "instances", s.Instances, "zoom", c.Zoom)
			reported = time.Now()
		}
	})
	if err != nil {
		logx.Fatalf("window run failed: %v", err)
	}
}

// setRamp fills the palette. Materials are numbered from zero; a cell carries
// PaletteOf(material), and a cell carrying zero draws nothing.
func setRamp(gfx *render.GPU, table *render.RampTable, phase float32) {
	for m := range materials {
		h := float64(m)/materials + float64(phase)*0.1
		table.Set(m,
			float32(0.5+0.5*math.Sin(2*math.Pi*h)),
			float32(0.5+0.5*math.Sin(2*math.Pi*(h+1.0/3))),
			float32(0.5+0.5*math.Sin(2*math.Pi*(h+2.0/3))),
			1)
	}
	gfx.SetRamp(table)
}
