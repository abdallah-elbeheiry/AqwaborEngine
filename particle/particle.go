// Package particle provides a GPU-instanced particle emitter.
// It demonstrates the new render.Renderer path: a shared quad mesh,
// an InstanceBuffer with dirty-range uploads, and a single DrawInstanced
// call per emitter per frame.
package particle

import (
	"math"
	"math/rand"

	"github.com/abdallah-elbeheiry/AqwaborEngine/render"
	"github.com/gogpu/wgpu"
)

// Particle is the per-particle state. Plain data only (no pointers).
type Particle struct {
	Pos     [2]float32
	Vel     [2]float32
	Life    float32 // remaining seconds
	MaxLife float32
	Color   [4]float32
	Scale   float32
	Rotate  float32
	RotVel  float32
	Active  bool
}

// EmitterConfig controls spawning behaviour.
type EmitterConfig struct {
	MaxParticles int
	EmitRate     float32 // particles per second
	Position     [2]float32
	Spread       float32 // radians
	Speed        float32
	SpeedVar     float32
	LifeMin      float32
	LifeMax      float32
	SizeMin      float32
	SizeMax      float32
	ColorStart   [4]float32
	ColorEnd     [4]float32
	Gravity      [2]float32
}

// Emitter manages a pool of particles and renders them via instanced draws.
type Emitter struct {
	cfg       EmitterConfig
	particles []Particle
	mesh      *render.Mesh
	buf       *render.InstanceBuffer
	acc       float32 // emit accumulator
}

// NewEmitter creates a particle emitter with the given config.
func NewEmitter(dev *wgpu.Device, cfg EmitterConfig) *Emitter {
	if cfg.MaxParticles <= 0 {
		cfg.MaxParticles = 1024
	}
	if cfg.EmitRate <= 0 {
		cfg.EmitRate = 100
	}
	if cfg.LifeMin <= 0 {
		cfg.LifeMin = 0.5
	}
	if cfg.LifeMax <= cfg.LifeMin {
		cfg.LifeMax = cfg.LifeMin + 1
	}
	if cfg.SizeMin <= 0 {
		cfg.SizeMin = 4
	}
	if cfg.SizeMax <= cfg.SizeMin {
		cfg.SizeMax = cfg.SizeMin + 4
	}
	if cfg.Speed <= 0 {
		cfg.Speed = 50
	}
	if cfg.Spread <= 0 {
		cfg.Spread = math.Pi * 2
	}

	return &Emitter{
		cfg:       cfg,
		particles: make([]Particle, cfg.MaxParticles),
		mesh:      render.NewUnitQuad(dev),
		buf:       render.NewInstanceBuffer(dev, cfg.MaxParticles),
	}
}

// Update advances all alive particles and spawns new ones.
// dt is seconds since last frame.
func (e *Emitter) Update(dt float32) {
	// Spawn
	e.acc += e.cfg.EmitRate * dt
	for e.acc >= 1 && e.spawnOne() {
		e.acc -= 1
	}

	// Update
	for i := range e.particles {
		p := &e.particles[i]
		if !p.Active {
			continue
		}
		p.Life -= dt
		if p.Life <= 0 {
			p.Active = false
			continue
		}
		p.Pos[0] += p.Vel[0] * dt
		p.Pos[1] += p.Vel[1] * dt
		p.Vel[0] += e.cfg.Gravity[0] * dt
		p.Vel[1] += e.cfg.Gravity[1] * dt
		p.Rotate += p.RotVel * dt
	}
}

// WriteInstances writes all alive particles into the instance buffer.
func (e *Emitter) WriteInstances() {
	idx := 0
	for i := range e.particles {
		p := &e.particles[i]
		if !p.Active {
			continue
		}
		t := 1 - p.Life/p.MaxLife
		color := lerpColor(e.cfg.ColorStart, e.cfg.ColorEnd, t)
		e.buf.Write(idx, &render.InstanceData{
			Position: p.Pos,
			Scale:    [2]float32{p.Scale, p.Scale},
			Rotation: p.Rotate,
			Color:    color,
			Layer:    0,
		})
		idx++
	}
}

// Flush uploads dirty instance data to the GPU.
func (e *Emitter) Flush(queue *wgpu.Queue) {
	e.buf.Flush(queue)
}

// Draw submits the instanced draw command.
func (e *Emitter) Draw(r *render.Renderer) {
	r.DrawInstanced(e.mesh, e.buf)
}

// Reset clears all particles and the instance buffer for the next frame.
func (e *Emitter) Reset() {
	e.buf.Reset()
}

// Mesh returns the shared quad mesh.
func (e *Emitter) Mesh() *render.Mesh { return e.mesh }

// Buffer returns the instance buffer.
func (e *Emitter) Buffer() *render.InstanceBuffer { return e.buf }

func (e *Emitter) spawnOne() bool {
	for i := range e.particles {
		if e.particles[i].Active {
			continue
		}
		p := &e.particles[i]
		angle := e.cfg.Position[0] + (rand.Float32()-0.5)*e.cfg.Spread
		speed := e.cfg.Speed + (rand.Float32()-0.5)*e.cfg.SpeedVar*2

		life := e.cfg.LifeMin + rand.Float32()*(e.cfg.LifeMax-e.cfg.LifeMin)
		size := e.cfg.SizeMin + rand.Float32()*(e.cfg.SizeMax-e.cfg.SizeMin)

		p.Pos = e.cfg.Position
		p.Vel = [2]float32{
			float32(math.Cos(float64(angle))) * speed,
			float32(math.Sin(float64(angle))) * speed,
		}
		p.Life = life
		p.MaxLife = life
		p.Color = e.cfg.ColorStart
		p.Scale = size
		p.Rotate = rand.Float32() * float32(math.Pi*2)
		p.RotVel = (rand.Float32() - 0.5) * 2
		p.Active = true
		return true
	}
	return false
}

func lerpColor(a, b [4]float32, t float32) [4]float32 {
	return [4]float32{
		a[0] + (b[0]-a[0])*t,
		a[1] + (b[1]-a[1])*t,
		a[2] + (b[2]-a[2])*t,
		a[3] + (b[3]-a[3])*t,
	}
}
