# Aqwabor

A 2D game engine in Go, developed alongside the games built on it.

Pure Go, `CGO_ENABLED=0`, rendering through [gogpu](https://github.com/gogpu/gogpu) on WebGPU.
macOS and Linux are the targets that have to be right; Windows is a "well, it works" tier.

```go
w := ecs.NewWorld()
pos := ecs.MustRegister[Position](w)

e := w.Create()
pos.Set(e, Position{X: 1})
pos.Wake(e)

pos.Each(func(e ecs.Entity, p *Position) { p.X++ })
```

Three things a caller touches, and everything else is behind them:

```
Window  = a surface, a device, a frame callback
GPU     = Begin, SetCamera, Draw*, End
World   = entities, plain components, and what is awake
```

The idea the rest follows from is that storage and scheduling are separate. A component store says
where a value lives; it does not decide who runs. What runs is the awake partition of a store, a
timer coming due, or a set a system keeps for itself — so a world where nothing is happening costs
close to nothing, rather than costing a scan to find that out.

## Documentation

[`docs/INDEX.md`](docs/INDEX.md) says which file answers which kind of question.
[`docs/ecs.md`](docs/ecs.md) is the one to read first.

## Building

```
go build ./...
go test ./...
go run ./cmd/aqwabor
```

## Releasing

A pushed semver tag is the release; there is no publish step. See
[`docs/releasing.md`](docs/releasing.md), and read it before cutting one.
