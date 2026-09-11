---
title: Index
tags: [engine, aqwabor, orientation]
---

What is in this directory, and which file answers which kind of question.

Aqwabor is a 2D game engine in Go. It is developed alongside the games built on it and is
authoritative: where it lacks something a game needs, the engine gains it rather than the game
working around it.

The whole of it is three things a caller touches.

```
Window  = a surface, a device, a frame callback
GPU     = Begin, SetCamera, Draw*, End
World   = entities, plain components, and what is awake
```

Everything else is behind those.

## Start here

- [The entity component system](ecs.md) — how a component is reached, what a system iterates, and
  what it means for an entity to be awake or waiting. Read this first; it is the shape the rest
  assumes.
- [Scheduling](scheduling.md) — deterministic master clock, dual advance API, per-job control, and how
  systems declare what they touch so the schedule can run them at the same time.
- [The render layer](render.md) — the frame's two phases, the Scene that draws a
  world, chunked instance buffers, GPU culling, strokes and the compact cell
  format. `examples/scenedemo` is the game side of it in one file.

## Subsystems

- [Window](window.md) — creating a surface and driving a frame.
- [The widget toolkit](ui.md) — labels, buttons, containers, alignment and themes.
- [Camera](camera.md) — the 2D view transform, as a component.
- [Map rendering](maprender.md) — an example, not engine API: map geometry, the
  level of detail a zoom is worth, and the worked case of a pipeline built
  outside the engine.
- [Input](input.md) — keys, mouse, actions and the bindings between them.
- [Sound](sound.md) — the context, clip and player model, and the volume rule.
- [Logging](logging.md) — levels, fields, components, and what a log line on a hot path costs.

## Releasing, and moving between versions

- [Releasing](releasing.md) — a tag is the release, what a version number promises, and why a
  published tag is never moved.
- [Migrating](migrating.md) — what v0.2.0 breaks and what to write instead.

## How this directory is meant to work

One subject per file, and one home per fact. A file that needs something another file owns links to
it rather than restating it, so there is one place to change when it moves.

These documents describe what the engine does. Why a thing is shaped the way it is belongs next to
the decision, which is the commit that made it: `git log` is the record, and the commit subjects are
what the release notes are built from.
