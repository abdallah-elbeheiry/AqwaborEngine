package main

import (
	"log"

	"github.com/abdallah-elbeheiry/AqwaborEngine/logx"
	"github.com/abdallah-elbeheiry/AqwaborEngine/schedulers"
	"github.com/abdallah-elbeheiry/AqwaborEngine/sound"
	"github.com/abdallah-elbeheiry/AqwaborEngine/ui"
)

func main() {
	logx.Init(logx.WithColor(true), logx.WithTimestamp(true), logx.WithLevel(logx.DebugLevel))

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

	runUIDemo()
}

func runUIDemo() {
	app, err := ui.New(ui.Config{
		Title:     "Aqwabor",
		W:         1280,
		H:         720,
		Resizable: true,
		Theme:     ui.LightPurple,
	})
	if err != nil {
		logx.Fatalf("ui: %v", err)
	}
	defer app.Close()

	s := schedulers.NewScheduler()
	s.Run(func(st schedulers.TickState) {}, 2.0)
	s.Start()
	defer s.Stop()

	var fox *ui.ImageAsset
	if foxAsset, err := app.Images().Load("examples/fox.png"); err != nil {
		logx.Warnf("image load (examples/fox.png): %v", err)
	} else {
		fox = foxAsset
	}

	themes := []*ui.Theme{
		ui.LightPurple, ui.DarkPurple, ui.Light, ui.Dark, ui.LightBlue, ui.DarkBlue,
	}
	idx := 0

	var build func()
	build = func() {
		children := []ui.Widget{
			ui.CenterText(ui.Label("Aqwabor Engine").FontSize(28).Bold()),
			ui.CenterText(ui.Label("UI layer over gogpu/ui")),
			app.Button("Ping", func() { logx.Info("ping") }),
			app.Button("Cycle Theme", func() {
				idx = (idx + 1) % len(themes)
				app.SetTheme(themes[idx])
				build()
			}),
		}
		if fox != nil {
			children = append(children,
				ui.ImageButton(fox, func() { logx.Info("fox image clicked") }),
			)
		}
		app.SetRoot(ui.Align(ui.Column(children...).
			Padding(24).Gap(12).Background(ui.SurfaceColor(app.Theme())), ui.CrossCenter))
	}
	build()

	if err := app.Run(); err != nil {
		log.Fatalf("ui run: %v", err)
	}

	if fox != nil {
		if ok := app.Images().TryRelease(fox); !ok {
			logx.Warn("fox asset still in use at shutdown")
		}
	}
}
