# sound — Usage

A small, engine-style audio package with a built-in mixer and CGO-free output
backends. It plays WAV (8/16/24/32-bit integer and 32-bit float) and MP3
(via the pure-Go `go-mp3` decoder) through the operating system's audio device.

## Concepts

| Type      | Role                                                                                |
|-----------|-------------------------------------------------------------------------------------|
| `Context` | Owns the audio device and master volume. One per application. Created with `New`.   |
| `Clip`    | A cached, decoded asset (WAV/MP3). Not audible by itself — play it to hear it.      |
| `Player`  | One playback instance of a `Clip`. Many `Player`s can play the same `Clip` at once. |

```
Context ──loads──▶ Clip ──Play/PlayLoop──▶ Player ──▶ mixer ──▶ device ──▶ speakers
            ▲                                         │
            └────────── master * clip * player ────────┘   (effective gain)
```

## Quick start

```go
package main

import "github.com/abdallah-elbeheiry/AqwaborEngine/sound"

func main() {
	// Open the device once and keep the Context alive for the whole program.
	snd, err := sound.New(sound.WithVolume(0.9))
	if err != nil {
		// err is only returned if the null fallback could not be created.
		panic(err)
	}
	defer snd.Close() // closing stops all playback

	clip, err := snd.LoadAudioFile("song.mp3")
	if err != nil {
		panic(err)
	}

	player, err := clip.Play()
	if err != nil {
		panic(err)
	}

	// ... run your game/render loop here; audio plays on its own goroutine ...

	player.Stop() // optional: stop early
}
```

> **Lifetime matters.** The `Context` owns the device and the mixer. If you
> close it (or let it be garbage collected) while you still expect to hear
> audio, playback stops immediately. In the example above the `defer snd.Close()`
> runs only when `main` returns, so the song keeps playing during the loop.

## Loading clips

```go
// From raw bytes (cached by SHA-256 of the content):
clip, _ := snd.LoadAudio(mp3Bytes)

// From a file (cached by normalised path):
clip, _ := snd.LoadAudioFile("music/theme.wav")
```

* Identical bytes / paths return the **same** `*Clip` — decoding is done once.
* Metadata is available without playing:
  ```go
  clip.SampleRate() // e.g. 44100
  clip.Channels()   // e.g. 2
  clip.Duration()   // time.Duration
  clip.Volume()     // default 1.0
  ```
* Unsupported/garbage input returns `sound.ErrUnsupportedFormat`.

## Playing

```go
p, _ := clip.Play()      // one-shot
pl, _ := clip.PlayLoop() // repeats until Stop
```

`Player` lifecycle methods:

| Method                  | Effect                                              |
|-------------------------|-----------------------------------------------------|
| `SetVolume(float64)`    | Set this instance's gain, clamped to `[0, 1]`.      |
| `Volume() float64`      | Current instance gain.                              |
| `EffectiveVolume()`     | Gain actually heard: `master * clip * player`.      |
| `Pause()` / `Resume()`  | Freeze / continue without losing position.          |
| `Stop()`                | End playback permanently (not restartable).         |

`Clip` also has `SetVolume` / `Volume`; a clip's volume applies to every
`Player` created from it (and to players already playing).

## Volume model

The gain the listener actually hears for a `Player` is:

```
effective = masterVolume * clipVolume * playerVolume     (each clamped to [0,1])
```

* `masterVolume` — set on the `Context` via `New(sound.WithVolume(v))` or
  `Context.SetMasterVolume(v)`. Applied by the mixer.
* `clipVolume` — set via `Clip.SetVolume(v)`.
* `playerVolume` — set via `Player.SetVolume(v)`.

A master volume of `0` silences everything regardless of the other factors.
`Player.EffectiveVolume()` returns the combined result.

## Context options

```go
sound.New(
	sound.WithVolume(0.9),     // initial master gain (default 1.0)
	sound.WithSampleRate(44100), // output rate in Hz (default 44100)
	sound.WithChannels(2),       // output channels (default 2 = stereo)
	sound.WithSilent(false),     // true => null driver, no real audio
)
```

## Backends

The output backend is chosen at build time per platform; the running program
does not select it. In every case an unavailable real device falls back to the
**null driver** (silent, but API-identical).

| Platform / build tag       | Backend                       | Fallback if unavailable |
|----------------------------|-------------------------------|-------------------------|
| Windows, macOS             | `oto/v3` (cgo)               | null                    |
| Linux (default)            | `jfreymuth/pulse` (pure Go)  | null                    |
| Linux `-tags otoaudio`*    | `oto/v3` (cgo)               | null                    |

\* only in a build that does **not** also link the wgpu graphics stack (see below).

So the real fallback chain is always **real device → null**, where the "real
device" is pulse on a default Linux build and oto elsewhere.

### Why Linux defaults to pulse (not oto)

`oto`'s ALSA backend is implemented with cgo and, like wgpu's `goffi` loader,
declares `//go:cgo_import_dynamic` for libc symbols. Linking **both** into one
binary triggers a Go toolchain error (`goffi_errno_location_stub: unhandled
relocation`, Go issue #50295). Because the graphics binary links wgpu, it cannot
also link oto — so the pure-Go pulse backend is the Linux default there.

If you build a binary that does **not** import wgpu (a headless audio tool, or
the `otoaudio` tag on a wgpu-free target), oto is used and falls back to null
when ALSA is missing. To use oto on Linux you therefore need a wgpu-free build;
you cannot swap it into the graphics binary.

### Null driver

When no real device is available (or `WithSilent(true)` is set), the engine uses
a null driver: it advances playback and restarts loops exactly as a real device
would, but produces no sound. This keeps the API identical for tests/headless and
guarantees playback never touches hardware.

## Concurrency & lifecycle

* `Context`, `Clip` and `Player` are safe for concurrent use.
* After `Context.Close()`, `LoadAudio`/`LoadAudioFile` and `Player` methods
  return `sound.ErrClosed`.
* `Close` is idempotent.
* The mixer advances voices on the device's own audio thread; calling
  `Play`/`Stop`/`Pause`/`Resume` from any goroutine is fine.

## Errors

| Error                        | Meaning                                            |
|------------------------------|----------------------------------------------------|
| `sound.ErrUnsupportedFormat` | Input could not be detected/decoded as WAV or MP3. |
| `sound.ErrClosed`            | Operation on a closed `Context`.                   |

## Minimal end-to-end example

```go
snd, _ := sound.New(sound.WithVolume(1.0))
defer snd.Close()

clip, _ := snd.LoadAudioFile("sfx/explosion.mp3")
clip.SetVolume(0.8)

// Overlapping sound effects from the same clip:
for i := 0; i < 5; i++ {
	go func() {
		p, _ := clip.Play()
		time.Sleep(300 * time.Millisecond)
		p.Stop()
	}()
}
```
