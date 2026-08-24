package main

import (
	_ "embed"

	"github.com/abdallah-elbeheiry/AqwaborEngine/logx"
	"github.com/abdallah-elbeheiry/AqwaborEngine/schedulers"
	"github.com/abdallah-elbeheiry/AqwaborEngine/sound"
	"github.com/abdallah-elbeheiry/AqwaborEngine/window"

	"github.com/gogpu/gogpu"
)

func main() {
	logx.Init(logx.WithColor(true), logx.WithTimestamp(true), logx.WithLevel(logx.TraceLevel))

	// Open the audio context once and keep it alive for the whole program so the
	// song keeps playing while the window demo runs. Closing it early (e.g. right
	// after Play) would silence the output before any audio is heard.
	snd, err := sound.New(sound.WithVolume(0.5))
	if err != nil {
		logx.Errorf("sound init (no audio device?): %v", err)
	} else {
		defer snd.Close()
		clip, err := snd.LoadAudioFile("examples/song-example.mp3")
		clip.SetVolume(1)
		if err != nil {
			logx.Errorf("sound load: %v", err)
		} else if p, err := clip.PlayLoop(); err != nil {
			logx.Errorf("sound play: %v", err)
		} else {
			logx.Info("playing song-example.mp3", "playerVolume", p.Volume(), "master", snd.MasterVolume())
		}
	}

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
	s.Run(func(st schedulers.TickState) {}, 2.0)
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
