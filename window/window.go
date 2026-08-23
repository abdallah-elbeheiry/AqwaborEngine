package window

import (
	_ "embed"
	"math"
	"sync"

	"github.com/abdallah-elbeheiry/AqwaborEngine/logx"
	"github.com/gogpu/gogpu"
	"github.com/gogpu/gputypes"
	"github.com/gogpu/wgpu"
)

// log is the window subsystem logger, pre-tagged with its component.
var log = logx.With("component", "window")

//go:embed shaders/vertex.wgsl
var vertexWGSL string

//go:embed shaders/fragment.wgsl
var fragmentWGSL string

//go:embed shaders/colored.wgsl
var coloredWGSL string

// Vertex is clip-space position + color.
type Vertex struct {
	X, Y       float32
	R, G, B, A float32
}

type WindowConfig struct {
	Title     string
	W, H      int
	Resizable bool
}

// Window wraps goGPU App on auto mode (GraphicsAPIAuto, RenderModeAuto).
type Window struct {
	app *gogpu.App
	cfg WindowConfig

	mu           sync.Mutex
	pendingBufs  []*wgpu.Buffer
	frameCleared bool
}

func NewWindow(cfg WindowConfig) (*Window, error) {
	if cfg.W <= 0 || cfg.H <= 0 {
		log.Warn("invalid size, falling back to default", "w", cfg.W, "h", cfg.H)
		cfg.W = 1280
		cfg.H = 720
	}
	if cfg.Title == "" {
		cfg.Title = "AqwaborEngine"
	}
	app := gogpu.NewApp(gogpu.DefaultConfig().
		WithTitle(cfg.Title).
		WithSize(cfg.W, cfg.H).
		WithResizable(cfg.Resizable))
	log.Info("window created", "title", cfg.Title, "w", cfg.W, "h", cfg.H, "resizable", cfg.Resizable)
	return &Window{app: app, cfg: cfg}, nil
}

func (w *Window) App() *gogpu.App { return w.app }

func (w *Window) Run(onDraw func(dc *gogpu.Context)) error {
	if w.app == nil {
		return nil
	}
	wrapped := func(dc *gogpu.Context) {
		w.mu.Lock()
		w.frameCleared = false
		w.mu.Unlock()
		log.Debug("frame begin")
		onDraw(dc)
		log.Debug("frame end")
	}
	w.app.OnDraw(wrapped)
	log.Info("window run loop starting")
	err := w.app.Run()
	if err != nil {
		log.Errorf("window run loop exited with error: %v", err)
	} else {
		log.Info("window run loop exited cleanly")
	}
	return err
}

func (w *Window) Close() {
	w.mu.Lock()
	defer w.mu.Unlock()
	released := 0
	for _, b := range w.pendingBufs {
		if b != nil {
			b.Release()
			released++
		}
	}
	w.pendingBufs = nil
	log.Info("window closed", "buffers_released", released)
}

func (w *Window) retainBuffer(b *wgpu.Buffer) {
	w.mu.Lock()
	w.pendingBufs = append(w.pendingBufs, b)
	total := len(w.pendingBufs)
	if total > 8 {
		toRelease := w.pendingBufs[0]
		w.pendingBufs = w.pendingBufs[1:]
		w.mu.Unlock()
		if toRelease != nil {
			toRelease.Release()
		}
		log.Debug("released oldest retained buffer", "live_buffers", len(w.pendingBufs))
		return
	}
	w.mu.Unlock()
	log.Debug("retained vertex buffer", "live_buffers", total)
}

func (w *Window) Draw(dc *gogpu.Context, vertices []Vertex) error {
	return w.drawVertices(dc, vertices)
}

func (w *Window) DrawPolygon(dc *gogpu.Context, vertices []Vertex) error {
	if len(vertices) < 3 {
		return nil
	}
	tris := triangulate(vertices)
	if tris == nil {
		return nil
	}
	return w.Draw(dc, tris)
}

// --- rendering ---

var coloredPipeline *wgpu.RenderPipeline
var pipelineErr error

func ensurePipeline(dev *wgpu.Device, format gputypes.TextureFormat) (*wgpu.RenderPipeline, error) {
	if coloredPipeline != nil {
		log.Debug("pipeline cache hit", "format", format, "cached_err", pipelineErr != nil)
		return coloredPipeline, pipelineErr
	}
	log.Debug("building render pipeline", "format", format)
	// Shaders are in independent files: shaders/vertex.wgsl and shaders/fragment.wgsl.
	// Fall back to combined colored.wgsl if those are empty (keeps single-file compat).
	vertSrc := vertexWGSL
	fragSrc := fragmentWGSL
	if vertSrc == "" || fragSrc == "" {
		vertSrc = coloredWGSL
		fragSrc = coloredWGSL
	}
	vertMod, err := dev.CreateShaderModule(&wgpu.ShaderModuleDescriptor{Label: "aqwabor vert", WGSL: vertSrc})
	if err != nil {
		pipelineErr = err
		log.Errorf("failed to create vertex shader module: %v", err)
		return nil, err
	}
	fragMod, err := dev.CreateShaderModule(&wgpu.ShaderModuleDescriptor{Label: "aqwabor frag", WGSL: fragSrc})
	if err != nil {
		pipelineErr = err
		log.Errorf("failed to create fragment shader module: %v", err)
		return nil, err
	}
	layout, err := dev.CreatePipelineLayout(&wgpu.PipelineLayoutDescriptor{Label: "aqwabor layout"})
	if err != nil {
		pipelineErr = err
		log.Errorf("failed to create pipeline layout: %v", err)
		return nil, err
	}
	coloredPipeline, err = dev.CreateRenderPipeline(&wgpu.RenderPipelineDescriptor{
		Label:  "aqwabor pipeline",
		Layout: layout,
		Vertex: wgpu.VertexState{
			Module:     vertMod,
			EntryPoint: "vs_main",
			Buffers: []gputypes.VertexBufferLayout{{
				ArrayStride: 24,
				StepMode:    gputypes.VertexStepModeVertex,
				Attributes: []gputypes.VertexAttribute{
					{Format: gputypes.VertexFormatFloat32x2, Offset: 0, ShaderLocation: 0},
					{Format: gputypes.VertexFormatFloat32x4, Offset: 8, ShaderLocation: 1},
				},
			}},
		},
		Fragment: &wgpu.FragmentState{
			Module:     fragMod,
			EntryPoint: "fs_main",
			Targets:    []gputypes.ColorTargetState{{Format: format, WriteMask: gputypes.ColorWriteMaskAll}},
		},
		Primitive: gputypes.PrimitiveState{Topology: gputypes.PrimitiveTopologyTriangleList},
	})
	pipelineErr = err
	if err != nil {
		log.Errorf("failed to create render pipeline: %v", err)
		return nil, err
	}
	log.Info("render pipeline ready", "format", format)
	return coloredPipeline, err
}

func (w *Window) drawVertices(dc *gogpu.Context, vertices []Vertex) error {
	if len(vertices) == 0 {
		return nil
	}
	provider := w.app.DeviceProvider()
	if provider == nil {
		log.Debug("draw skipped: no device provider")
		return nil
	}
	dev := provider.Device()
	queue := provider.Queue()
	format := provider.SurfaceFormat()
	if dev == nil || queue == nil {
		log.Debug("draw skipped: device or queue not ready", "has_device", dev != nil, "has_queue", queue != nil)
		return nil
	}
	pipe, err := ensurePipeline(dev, format)
	if err != nil || pipe == nil {
		return err
	}
	size := uint64(len(vertices) * 24)
	buf, err := dev.CreateBuffer(&wgpu.BufferDescriptor{
		Label: "aqwabor vbuf",
		Size:  size,
		Usage: gputypes.BufferUsageVertex | gputypes.BufferUsageCopyDst,
	})
	if err != nil {
		log.Errorf("failed to create vertex buffer: %v", err)
		return err
	}
	w.retainBuffer(buf)

	b := vertexSliceToBytes(vertices, int(size))
	if err := queue.WriteBuffer(buf, 0, b); err != nil {
		log.Errorf("failed to upload vertices: %v", err)
		return err
	}
	enc := dc.CommandEncoder()
	if enc == nil {
		log.Debug("draw skipped: no command encoder")
		return nil
	}
	view := dc.SurfaceView()
	if view == nil {
		log.Debug("draw skipped: no surface view")
		return nil
	}
	w.mu.Lock()
	isFirst := !w.frameCleared
	w.frameCleared = true
	w.mu.Unlock()

	loadOp := gputypes.LoadOpLoad
	clearVal := gputypes.Color{}
	if isFirst {
		loadOp = gputypes.LoadOpClear
		clearVal = gputypes.Color{R: 0.05, G: 0.05, B: 0.1, A: 1}
	}
	pass, err := enc.BeginRenderPass(&wgpu.RenderPassDescriptor{
		ColorAttachments: []wgpu.RenderPassColorAttachment{{
			View: view, LoadOp: loadOp, StoreOp: gputypes.StoreOpStore, ClearValue: clearVal,
		}},
	})
	if err != nil {
		log.Errorf("failed to begin render pass: %v", err)
		return err
	}
	pass.SetPipeline(pipe)
	pass.SetVertexBuffer(0, buf, 0)
	pass.Draw(uint32(len(vertices)), 1, 0, 0)
	log.Debug("draw submitted", "vertices", len(vertices), "bytes", size, "first_clear", isFirst)
	return pass.End()
}

func vertexSliceToBytes(verts []Vertex, byteLen int) []byte {
	out := make([]byte, byteLen)
	for i, v := range verts {
		off := i * 24
		putF32(out[off+0:], v.X)
		putF32(out[off+4:], v.Y)
		putF32(out[off+8:], v.R)
		putF32(out[off+12:], v.G)
		putF32(out[off+16:], v.B)
		putF32(out[off+20:], v.A)
	}
	return out
}

func putF32(b []byte, v float32) {
	bits := math.Float32bits(v)
	b[0] = byte(bits)
	b[1] = byte(bits >> 8)
	b[2] = byte(bits >> 16)
	b[3] = byte(bits >> 24)
}

// --- triangulate (ear clipping) ---

func orient(a, b, c Vertex) float32 { return (b.X-a.X)*(c.Y-a.Y) - (b.Y-a.Y)*(c.X-a.X) }

func signedArea(poly []Vertex) float32 {
	var area float32
	for i := range poly {
		p := poly[i]
		q := poly[(i+1)%len(poly)]
		area += (q.X - p.X) * (q.Y + p.Y)
	}
	return area
}

func pointInTriangle(p, a, b, c Vertex) bool {
	d1 := orient(a, b, p)
	d2 := orient(b, c, p)
	d3 := orient(c, a, p)
	hasNeg := d1 < 0 || d2 < 0 || d3 < 0
	hasPos := d1 > 0 || d2 > 0 || d3 > 0
	return !(hasNeg && hasPos)
}

func triangulate(poly []Vertex) []Vertex {
	n := len(poly)
	if n < 3 {
		log.Debug("triangulate skipped: need >=3 verts", "n", n)
		return nil
	}
	log.Debug("triangulating polygon", "input_verts", n)
	verts := append([]Vertex{}, poly...)
	if signedArea(verts) >= 0 {
		for i, j := 0, len(verts)-1; i < j; i, j = i+1, j-1 {
			verts[i], verts[j] = verts[j], verts[i]
		}
	}
	idx := make([]int, n)
	for i := range idx {
		idx[i] = i
	}
	result := make([]Vertex, 0, (n-2)*3)
	for len(idx) > 3 {
		clipped := false
		for i := 0; i < len(idx); i++ {
			m := len(idx)
			prevI := (i - 1 + m) % m
			nextI := (i + 1) % m
			prev := verts[idx[prevI]]
			curr := verts[idx[i]]
			next := verts[idx[nextI]]
			if orient(prev, curr, next) <= 0 {
				continue
			}
			inside := false
			for j := range m {
				if j == prevI || j == i || j == nextI {
					continue
				}
				if pointInTriangle(verts[idx[j]], prev, curr, next) {
					inside = true
					break
				}
			}
			if inside {
				continue
			}
			result = append(result, prev, curr, next)
			idx = append(idx[:i], idx[i+1:]...)
			clipped = true
			break
		}
		if !clipped {
			log.Debug("triangulate failed: polygon not simple", "input_verts", n)
			return nil
		}
	}
	result = append(result, verts[idx[0]], verts[idx[1]], verts[idx[2]])
	log.Debug("triangulate complete", "input_verts", n, "output_tris", len(result)/3)
	return result
}
