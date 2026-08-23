package main

import (
	"os"

	"github.com/abdallah-elbeheiry/AqwaborEngine/logx"
	"github.com/abdallah-elbeheiry/AqwaborEngine/schedulers"
	"github.com/abdallah-elbeheiry/AqwaborEngine/window"

	"github.com/gogpu/gogpu"
)

func main() {
	logx.Init(logx.WithColor(true), logx.WithOutput(os.Stdout), logx.WithLevel(logx.DebugLevel))
	runWindowDemo()
}

func runWindowDemo() {
	win, err := window.NewWindow(window.WindowConfig{
		Title:     "Aqwabor Engine - goGPU Auto",
		W:         1280,
		H:         720,
		Resizable: true,
	})
	if err != nil {
		logx.Fatalf("failed to create window: %v", err)
	}
	defer win.Close()
	logx.Info("window ready", "title", "Aqwabor Engine - goGPU Auto", "w", 1280, "h", 720)

	s := schedulers.NewScheduler()
	s.Run(func(st schedulers.TickState) {}, 60.0)
	s.Start()
	defer s.Stop()

	quad := []window.Vertex{
		{X: -0.5, Y: -0.5, R: 1.0, G: 0.0, B: 0.0, A: 1},
		{X: 0.5, Y: -0.5, R: 0.0, G: 1.0, B: 0.0, A: 1},
		{X: 0.5, Y: 0.5, R: 0.0, G: 0.0, B: 1.0, A: 1},
		{X: -0.5, Y: 0.5, R: 1.0, G: 1.0, B: 0.0, A: 1},
	}

	if err := win.Run(func(dc *gogpu.Context) {
		dc.Clear(0.05, 0.05, 0.1, 1)
		if err := win.DrawPolygon(dc, quad); err != nil {
			logx.Errorf("draw: %v", err)
		}
	}); err != nil {
		logx.Fatalf("window run failed: %v", err)
	}
}
