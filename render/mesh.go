package render

import (
	"unsafe"

	"github.com/gogpu/gputypes"
	"github.com/gogpu/wgpu"
)

// MeshVertex is the per-vertex data for the mesh geometry itself.
type MeshVertex struct {
	X, Y       float32
	R, G, B, A float32
}

// MeshVertexLayout describes the vertex buffer layout for the mesh (locations 0-1).
var MeshVertexLayout = gputypes.VertexBufferLayout{
	ArrayStride: 24,
	StepMode:    gputypes.VertexStepModeVertex,
	Attributes: []gputypes.VertexAttribute{
		{Format: gputypes.VertexFormatFloat32x2, Offset: 0, ShaderLocation: 0},
		{Format: gputypes.VertexFormatFloat32x4, Offset: 8, ShaderLocation: 1},
	},
}

// Mesh is a shared, immutable geometry (vertex + index buffers).
type Mesh struct {
	VertexBuffer *wgpu.Buffer
	IndexBuffer  *wgpu.Buffer
	IndexCount   uint32
}

// NewMesh creates a Mesh from raw vertex and index data.
// queue is used for the initial upload; if nil, dev.Queue() is used.
func NewMesh(dev *wgpu.Device, queue *wgpu.Queue, vertices []MeshVertex, indices []uint32) *Mesh {
	if len(vertices) == 0 || len(indices) == 0 {
		panic("mesh requires at least one vertex and one index")
	}
	if queue == nil {
		queue = dev.Queue()
	}

	vertBuf, err := dev.CreateBuffer(&wgpu.BufferDescriptor{
		Label: "mesh vertices",
		Size:  uint64(len(vertices) * 24),
		Usage: gputypes.BufferUsageVertex | gputypes.BufferUsageCopyDst,
	})
	if err != nil {
		panic(err)
	}
	vertBytes := vertexBytes(vertices)
	if err := queue.WriteBuffer(vertBuf, 0, vertBytes); err != nil {
		panic(err)
	}

	idxBuf, err := dev.CreateBuffer(&wgpu.BufferDescriptor{
		Label: "mesh indices",
		Size:  uint64(len(indices) * 4),
		Usage: gputypes.BufferUsageIndex | gputypes.BufferUsageCopyDst,
	})
	if err != nil {
		panic(err)
	}
	idxBytes := indexBytes(indices)
	if err := queue.WriteBuffer(idxBuf, 0, idxBytes); err != nil {
		panic(err)
	}

	return &Mesh{
		VertexBuffer: vertBuf,
		IndexBuffer:  idxBuf,
		IndexCount:   uint32(len(indices)),
	}
}

// NewUnitQuad creates a 1×1 quad centred at the origin (vertices -0.5 to +0.5).
// queue may be nil (falls back to dev.Queue()).
func NewUnitQuad(dev *wgpu.Device, queue *wgpu.Queue) *Mesh {
	vertices := []MeshVertex{
		{-0.5, -0.5, 1, 1, 1, 1},
		{0.5, -0.5, 1, 1, 1, 1},
		{0.5, 0.5, 1, 1, 1, 1},
		{-0.5, 0.5, 1, 1, 1, 1},
	}
	indices := []uint32{0, 1, 2, 0, 2, 3}
	return NewMesh(dev, queue, vertices, indices)
}

// Release releases GPU resources.
func (m *Mesh) Release() {
	if m.VertexBuffer != nil {
		m.VertexBuffer.Release()
	}
	if m.IndexBuffer != nil {
		m.IndexBuffer.Release()
	}
}

func vertexBytes(verts []MeshVertex) []byte {
	b := make([]byte, len(verts)*24)
	for i, v := range verts {
		off := i * 24
		putF32(b[off:], v.X)
		putF32(b[off+4:], v.Y)
		putF32(b[off+8:], v.R)
		putF32(b[off+12:], v.G)
		putF32(b[off+16:], v.B)
		putF32(b[off+20:], v.A)
	}
	return b
}

func indexBytes(indices []uint32) []byte {
	b := make([]byte, len(indices)*4)
	for i, idx := range indices {
		off := i * 4
		b[off] = byte(idx)
		b[off+1] = byte(idx >> 8)
		b[off+2] = byte(idx >> 16)
		b[off+3] = byte(idx >> 24)
	}
	return b
}

func putF32(b []byte, v float32) {
	bits := uint32(0)
	*(*float32)(unsafe.Pointer(&bits)) = v
	b[0] = byte(bits)
	b[1] = byte(bits >> 8)
	b[2] = byte(bits >> 16)
	b[3] = byte(bits >> 24)
}
