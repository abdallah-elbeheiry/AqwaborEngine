package render

import "github.com/gogpu/wgpu"

// SpriteBatch is a convenience wrapper around InstanceBuffer for sprite
// rendering. It hides the raw mesh + buffer management behind a simple
// Set/SetAll interface. Created via GPU.Sprites().
type SpriteBatch struct {
	mesh *Mesh
	buf  *InstanceBuffer
}

// NewSpriteBatch creates a sprite batch with the given capacity.
// mesh is the shared quad geometry; if nil, a unit quad is created.
func NewSpriteBatch(dev *wgpu.Device, queue *wgpu.Queue, mesh *Mesh, capacity int) *SpriteBatch {
	if mesh == nil {
		mesh = NewUnitQuad(dev, queue)
	}
	return &SpriteBatch{
		mesh: mesh,
		buf:  NewInstanceBuffer(dev, capacity),
	}
}

// Set writes a single sprite at the given index.
func (sb *SpriteBatch) Set(index int, sprite InstanceData) {
	sb.buf.Write(index, &sprite)
}

// SetAll replaces all sprites in the batch (dense write, one GPU upload).
func (sb *SpriteBatch) SetAll(sprites []InstanceData) {
	sb.buf.WriteAll(sprites)
}

// Count returns the number of sprites written this frame.
func (sb *SpriteBatch) Count() int { return sb.buf.Count() }

// Capacity is how many instances the batch holds without growing. Writing past
// it grows the buffer rather than panicking, so this is a hint for a caller that
// would rather cap its own work than reallocate.
func (sb *SpriteBatch) Capacity() int { return sb.buf.Capacity() }

// BeginFrame releases buffers left behind by earlier growth, once enough frames
// have passed that no submitted frame can still be reading them.
func (sb *SpriteBatch) BeginFrame() { sb.buf.BeginFrame() }

// Reset clears the batch for the next frame.
func (sb *SpriteBatch) Reset() { sb.buf.Reset() }

// mesh returns the batch's mesh (internal use by GPU.DrawSprites).
func (sb *SpriteBatch) meshInternal() *Mesh { return sb.mesh }

// buffer returns the batch's instance buffer (internal use by GPU.DrawSprites).
func (sb *SpriteBatch) bufferInternal() *InstanceBuffer { return sb.buf }

// Release frees GPU resources.
func (sb *SpriteBatch) Release() {
	if sb.mesh != nil {
		sb.mesh.Release()
		sb.mesh = nil
	}
	if sb.buf != nil {
		// The buffer owns GPU memory: the live allocation plus anything growth
		// retired. Dropping the reference without releasing leaked both.
		sb.buf.Release()
		sb.buf = nil
	}
}
