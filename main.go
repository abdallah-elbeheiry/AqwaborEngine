package main

import (
	"log"

	"github.com/gogpu/gogpu"
)

func main() {
	runWindowDemo()
}

func runWindowDemo() {
	win, err := NewWindow(WindowConfig{Title: "Aqwabor Engine - goGPU Auto", W: 1280, H: 720, Resizable: true})
	if err != nil {
		log.Fatal(err)
	}
	defer win.Close()

	loop := NewLoop(60)
	loop.Do(func() {})
	loop.Start()
	defer loop.Stop()

	quad := []Vertex{
		{X: -0.5, Y: -0.5, R: 1.0, G: 0.0, B: 0.0, A: 1},
		{X: 0.5, Y: -0.5, R: 0.0, G: 1.0, B: 0.0, A: 1},
		{X: 0.5, Y: 0.5, R: 0.0, G: 0.0, B: 1.0, A: 1},
		{X: -0.5, Y: 0.5, R: 1.0, G: 1.0, B: 0.0, A: 1},
	}

	if err := win.Run(func(dc *gogpu.Context) {
		dc.Clear(0.05, 0.05, 0.1, 1)
		if err := win.DrawPolygon(dc, quad); err != nil {
			log.Printf("draw: %v", err)
		}
	}); err != nil {
		log.Fatal(err)
	}
}
