package ui

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/srwiley/oksvg"
	"github.com/srwiley/rasterx"
	"golang.org/x/image/draw"
)

// ImageFit controls how an image is scaled to fit its allocated bounds.
type ImageFit int

const (
	// Fill stretches the image to exactly fill the bounds (may distort aspect ratio).
	Fill ImageFit = iota
	// Contain fits the image fully inside the bounds, preserving aspect ratio.
	Contain
	// Cover fills the bounds, preserving aspect ratio and cropping overflow.
	Cover
	// None draws the image at its natural size (top-left anchored, may overflow).
	None
)

// ImageAsset is an opaque handle to a loaded image resource. It is returned by
// ImageManager.Load and consumed by Image / ImageButton / Clickable. Treat it as
// an opaque token: never reach into its fields and never hold a raw copy of the
// underlying pixel data past the asset's lifetime.
//
// Asset lifetime is explicit and deterministic:
//
//	asset, err := images.Load("logo.png")
//	root  = ui.Image(asset)            // asset now has 1 active user
//	images.TryRelease(asset)           // refuses while still used
//	// ...remove the widget from the tree (Unmount)...
//	images.TryRelease(asset)           // ok: resource freed
type ImageAsset struct {
	mu          sync.Mutex
	manager     *ImageManager // immutable: the manager that owns this asset
	path        string        // immutable: normalized source path (diagnostics only)
	img         image.Image   // guarded by mu; nil once released
	width       int           // immutable: natural pixel width
	height      int           // immutable: natural pixel height
	released    bool          // guarded by mu
	activeUsers int           // guarded by mu
}

// IsReleased reports whether the asset's resource has been released (via
// TryRelease or ForceRelease). A released asset must not be rendered.
func (a *ImageAsset) IsReleased() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.released
}

// Users returns the current number of active consumers referencing the asset.
func (a *ImageAsset) Users() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.activeUsers
}

// Path returns the normalized source path the asset was loaded from.
func (a *ImageAsset) Path() string { return a.path }

// Size returns the natural pixel dimensions of the asset.
func (a *ImageAsset) Size() (int, int) { return a.width, a.height }

func (a *ImageAsset) acquire() {
	a.mu.Lock()
	a.activeUsers++
	a.mu.Unlock()
}

func (a *ImageAsset) releaseUser() int {
	a.mu.Lock()
	if a.activeUsers > 0 {
		a.activeUsers--
	}
	n := a.activeUsers
	a.mu.Unlock()
	return n
}

// take returns the pixel data and whether it is still valid (not released).
// It is the only supported way for a widget to read the image, so that a
// concurrent ForceRelease cannot be observed mid-draw as a use-after-free.
func (a *ImageAsset) take() (image.Image, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.img, !a.released
}

// ImageManager loads, caches and owns image assets. It is the single owner of
// image resources; widgets only consume assets handed out by a manager.
//
// Typical integration: app.Images(). A standalone manager can also be created
// with ui.NewImageManager().
type ImageManager struct {
	mu    sync.Mutex
	cache map[string]*ImageAsset // keyed by normalized source path
}

// NewImageManager creates an empty image manager. Decoding is CPU-only, so a
// manager can be constructed before any window/GPU context exists.
func NewImageManager() *ImageManager {
	return &ImageManager{cache: make(map[string]*ImageAsset)}
}

// Load reads, decodes and uploads the image at path. Repeated loads of the same
// normalized source return the same canonical asset without re-decoding or
// duplicating the resource. Use TryRelease / ForceRelease to free it.
//
// Supported formats are PNG, JPEG (.jpg/.jpeg) and SVG (.svg/.svgz). SVG is
// rasterized once at load time (see decodeSVG) into a fixed-resolution bitmap,
// so it behaves like any other raster image from the widget's point of view.
// Malformed or missing files return an error.
func (m *ImageManager) Load(path string) (*ImageAsset, error) {
	norm, err := normalizePath(path)
	if err != nil {
		return nil, err
	}

	// Fast path: already cached and live.
	if a := m.lookupLive(norm); a != nil {
		return a, nil
	}

	img, err := decodeImage(norm)
	if err != nil {
		return nil, err
	}
	b := img.Bounds()
	a := &ImageAsset{
		manager: m,
		path:    norm,
		img:     img,
		width:   b.Dx(),
		height:  b.Dy(),
	}

	// Insert under the same lock order (manager → asset) used by release, so
	// a concurrent release cannot race the insert.
	m.mu.Lock()
	defer m.mu.Unlock()
	if existing := m.lookupLiveLocked(norm); existing != nil {
		return existing, nil
	}
	m.cache[norm] = a
	log.Debug("image loaded", "path", norm, "w", a.width, "h", a.height)
	return a, nil
}

// lookupLive returns the cached asset for norm if it exists and is not released.
func (m *ImageManager) lookupLive(norm string) *ImageAsset {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.lookupLiveLocked(norm)
}

func (m *ImageManager) lookupLiveLocked(norm string) *ImageAsset {
	a, ok := m.cache[norm]
	if !ok {
		return nil
	}
	a.mu.Lock()
	released := a.released
	a.mu.Unlock()
	if released {
		return nil
	}
	return a
}

// TryRelease releases the asset only if it has no active users. It returns true
// on success. If the asset is still used by one or more widgets, or has already
// been released, or is owned by another manager, it returns false and leaves
// the resource intact.
func (m *ImageManager) TryRelease(asset *ImageAsset) bool {
	if asset == nil {
		return false
	}
	if asset.manager != m {
		log.Warn("image: TryRelease on asset owned by another manager", "path", asset.path)
		return false
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.cache[asset.path]; !ok {
		// Not in our cache → already released (or never ours).
		log.Debug("image: TryRelease on already-released asset", "path", asset.path)
		return false
	}

	asset.mu.Lock()
	users := asset.activeUsers
	if users > 0 {
		asset.mu.Unlock()
		log.Warn("image: cannot release asset still in use",
			"path", asset.path, "users", users)
		return false
	}
	asset.img = nil
	asset.released = true
	asset.mu.Unlock()

	delete(m.cache, asset.path)
	log.Debug("image: released", "path", asset.path)
	return true
}

// ForceRelease destroys the asset's resource immediately, even if active users
// still reference it. This is intentionally unsafe: widgets that keep using the
// released asset will render nothing (deterministic), but they are now holding a
// dead reference. The caller is responsible for removing/replacing those users.
//
// Calling ForceRelease twice (or after TryRelease) is a no-op and never frees a
// resource twice.
func (m *ImageManager) ForceRelease(asset *ImageAsset) {
	if asset == nil {
		return
	}
	if asset.manager != m {
		log.Warn("image: ForceRelease on asset owned by another manager", "path", asset.path)
		return
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.cache[asset.path]; !ok {
		// Already released → no double free.
		log.Debug("image: ForceRelease on already-released asset", "path", asset.path)
		return
	}

	asset.mu.Lock()
	asset.img = nil
	asset.released = true
	asset.mu.Unlock()

	delete(m.cache, asset.path)
	log.Warn("image: force-released asset (active users invalidated)",
		"path", asset.path, "users", asset.activeUsers)
}

func normalizePath(path string) (string, error) {
	if strings.TrimSpace(path) == "" {
		return "", fmt.Errorf("image: empty path")
	}
	clean := filepath.Clean(path)
	abs, err := filepath.Abs(clean)
	if err != nil {
		return clean, nil
	}
	return abs, nil
}

func decodeImage(path string) (image.Image, error) {
	ext := strings.ToLower(filepath.Ext(path))
	var (
		img image.Image
		err error
	)
	switch ext {
	case ".png":
		img, err = decodePNG(path)
	case ".jpg", ".jpeg":
		img, err = decodeJPEG(path)
	case ".svg", ".svgz":
		img, err = decodeSVG(path)
	default:
		return nil, fmt.Errorf("image: unsupported format %q (supported: png, jpeg, svg, svgz)", path)
	}
	if err != nil {
		return nil, err
	}
	return toRGBA(img), nil
}

func decodePNG(path string) (image.Image, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("image: open %q: %w", path, err)
	}
	defer f.Close()
	img, err := png.Decode(f)
	if err != nil {
		return nil, fmt.Errorf("image: decode png %q: %w", path, err)
	}
	return img, nil
}

func decodeJPEG(path string) (image.Image, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("image: open %q: %w", path, err)
	}
	defer f.Close()
	img, err := jpeg.Decode(f)
	if err != nil {
		return nil, fmt.Errorf("image: decode jpeg %q: %w", path, err)
	}
	return img, nil
}

// decodeSVG parses an SVG (or gzipped .svgz) and rasterizes it into a bitmap.
// SVGs are vector and have no intrinsic pixel size, so we pick a raster size
// from the viewBox (scaled into a sane range) and render once. The result is a
// plain *image.RGBA, identical in shape to a decoded PNG/JPEG.
func decodeSVG(path string) (image.Image, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("image: read %q: %w", path, err)
	}
	if strings.EqualFold(filepath.Ext(path), ".svgz") {
		if gz, gerr := gzip.NewReader(bytes.NewReader(data)); gerr == nil {
			if dec, derr := io.ReadAll(gz); derr == nil {
				data = dec
			}
			_ = gz.Close()
		}
	}

	icon, err := oksvg.ReadIconStream(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("image: parse svg %q: %w", path, err)
	}
	if icon.ViewBox.W <= 0 || icon.ViewBox.H <= 0 {
		icon.ViewBox.W, icon.ViewBox.H = 256, 256
	}
	w, h := svgRasterSize(icon.ViewBox.W, icon.ViewBox.H)
	icon.SetTarget(0, 0, float64(w), float64(h))

	img := image.NewRGBA(image.Rect(0, 0, w, h))
	scanner := rasterx.NewScannerGV(w, h, img, img.Bounds())
	raster := rasterx.NewDasher(w, h, scanner)
	icon.Draw(raster, 1.0)
	return img, nil
}

// svgRasterSize chooses a raster resolution for an SVG viewBox. Tiny icons are
// scaled up so they stay crisp when displayed larger; very large viewBoxes are
// capped to bound memory use. Aspect ratio is preserved.
func svgRasterSize(vbW, vbH float64) (int, int) {
	const minSize, maxSize = 512.0, 4096.0
	scale := 1.0
	m := math.Max(vbW, vbH)
	if m < minSize {
		scale = minSize / m
	}
	if m*scale > maxSize {
		scale = maxSize / m
	}
	return int(math.Round(vbW * scale)), int(math.Round(vbH * scale))
}

// toRGBA normalizes any decoded image into a non-premultiplied *image.RGBA so
// the rendering and scaling paths have a single, predictable pixel format.
func toRGBA(src image.Image) *image.RGBA {
	if r, ok := src.(*image.RGBA); ok && r.Stride == r.Rect.Dx()*4 {
		return r
	}
	b := src.Bounds()
	out := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	draw.Draw(out, out.Bounds(), src, b.Min, draw.Src)
	return out
}
