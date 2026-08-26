package ui

import (
	"image"

	"github.com/gogpu/ui/event"
	"github.com/gogpu/ui/geometry"
	"github.com/gogpu/ui/widget"
	"golang.org/x/image/draw"
)

// ImageWidget is the builder returned by Image. It supports fluent configuration
// (Size / Width / Height / Fit / Opacity) and composes with the existing layout
// widgets (Row, Column, Box, Clickable, ...).
type ImageWidget = *imageWidget

// imageWidget renders a single loaded ImageAsset. It is a leaf widget that
// registers itself as an active user of the asset on Mount and unregisters on
// Unmount, so the asset's lifetime stays deterministic and tied to the real
// widget-tree lifecycle (not to construction or garbage collection).
type imageWidget struct {
	widget.WidgetBase

	asset   *ImageAsset
	width   float32
	height  float32
	fit     ImageFit
	opacity float32

	// derived caches the scaled/opacity-adjusted image between draws so that
	// repeated frames don't re-scale. It is view-only; the shared asset data
	// is never duplicated per widget.
	derived *image.RGBA
	dW      int
	dH      int
	dOp     float32
}

// Image creates an image widget that consumes the given asset. The asset must be
// loaded via ImageManager.Load; widgets never perform I/O or decoding.
func Image(asset *ImageAsset) ImageWidget {
	if asset == nil {
		// A nil asset renders nothing but keeps the call type-safe.
		return &imageWidget{opacity: 1, fit: Contain}
	}
	w := &imageWidget{
		asset:   asset,
		fit:     Contain,
		opacity: 1,
	}
	w.SetVisible(true)
	w.SetEnabled(true)
	return w
}

// Size sets explicit width and height (in logical pixels). Zero keeps the
// natural dimension on that axis.
func (w ImageWidget) Size(width, height int) ImageWidget {
	w.width = float32(width)
	w.height = float32(height)
	return w
}

// Width sets an explicit width in logical pixels.
func (w ImageWidget) Width(width int) ImageWidget {
	w.width = float32(width)
	return w
}

// Height sets an explicit height in logical pixels.
func (w ImageWidget) Height(height int) ImageWidget {
	w.height = float32(height)
	return w
}

// Fit sets how the image is scaled within its bounds.
func (w ImageWidget) Fit(fit ImageFit) ImageWidget {
	w.fit = fit
	return w
}

// OnClick makes the image directly clickable. It is a convenience shorthand for
// ui.Clickable(image, fn): clicks anywhere within the image's bounds invoke fn.
// The returned Widget is no longer an ImageWidget, so chain OnClick last.
func (w ImageWidget) OnClick(fn func()) Widget {
	return Clickable(w, fn)
}

// Opacity sets the image opacity in [0,1]. Values < 1 are baked into a derived
// copy at draw time.
func (w ImageWidget) Opacity(alpha float32) ImageWidget {
	if alpha < 0 {
		alpha = 0
	}
	if alpha > 1 {
		alpha = 1
	}
	w.opacity = alpha
	return w
}

// Layout sizes the widget from the asset's natural dimensions, explicit size
// overrides, and the configured fit mode. A released or nil asset has zero size.
func (w *imageWidget) Layout(_ widget.Context, c geometry.Constraints) geometry.Size {
	natural := geometry.Sz(0, 0)
	if w.asset != nil && !w.asset.IsReleased() {
		natural = geometry.Sz(float32(w.asset.width), float32(w.asset.height))
	}

	desired := natural
	if w.width > 0 {
		desired.Width = w.width
	}
	if w.height > 0 {
		desired.Height = w.height
	}

	switch w.fit {
	case Contain:
		if desired.Width > 0 && desired.Height > 0 {
			m := c.BiggestFinite(desired.Width, desired.Height)
			desired = desired.FitIn(m)
		}
	case Cover:
		if desired.Width > 0 && desired.Height > 0 {
			m := c.BiggestFinite(desired.Width, desired.Height)
			desired = desired.FillIn(m).Min(m)
		}
	case Fill:
		if c.HasBoundedWidth() && w.width <= 0 {
			desired.Width = c.MaxWidth
		}
		if c.HasBoundedHeight() && w.height <= 0 {
			desired.Height = c.MaxHeight
		}
	case None:
		// natural size; may overflow bounds (clipped by parent).
	}

	result := c.Constrain(desired)
	w.SetBounds(geometry.FromPointSize(w.Position(), result))
	return result
}

// Draw renders the asset. If the asset is released (e.g. after ForceRelease) or
// has no data, it draws nothing — a deterministic, crash-free no-op.
func (w *imageWidget) Draw(_ widget.Context, canvas widget.Canvas) {
	if !w.IsVisible() {
		return
	}
	if w.asset == nil {
		return
	}
	img, ok := w.asset.Take()
	if !ok || img == nil {
		return
	}
	b := w.Bounds()
	if b.IsEmpty() {
		return
	}
	src := w.derive(img, int(b.Width()), int(b.Height()))
	if src == nil {
		return
	}
	canvas.DrawImage(src, b.Min)
}

// Event: images are not interactive. Returning false lets events propagate.
func (w *imageWidget) Event(_ widget.Context, _ event.Event) bool { return false }

// Children: leaf widget.
func (w *imageWidget) Children() []widget.Widget { return nil }

// Mount registers the widget as an active user of its asset. Implements
// widget.Lifecycle. Called by the toolkit when the widget enters the tree.
func (w *imageWidget) Mount(_ widget.Context) {
	if w.asset != nil {
		w.asset.Acquire()
	}
}

// Unmount unregisters the widget, freeing the asset for release when no other
// users remain. Implements widget.Lifecycle.
func (w *imageWidget) Unmount() {
	if w.asset != nil {
		w.asset.ReleaseUser()
	}
}

// derive scales the source image to (bw,bh) per the fit mode and applies
// opacity, returning a cached result when inputs are unchanged.
func (w *imageWidget) derive(src image.Image, bw, bh int) image.Image {
	if bw <= 0 || bh <= 0 {
		return src
	}
	sw, sh := src.Bounds().Dx(), src.Bounds().Dy()
	if sw <= 0 || sh <= 0 {
		return src
	}

	var dw, dh int
	switch w.fit {
	case Fill:
		dw, dh = bw, bh
	case Contain:
		scale := min(float64(bw)/float64(sw), float64(bh)/float64(sh))
		dw = clampDim(float64(sw) * scale)
		dh = clampDim(float64(sh) * scale)
	case Cover:
		scale := max(float64(bw)/float64(sw), float64(bh)/float64(sh))
		dw = clampDim(float64(sw) * scale)
		dh = clampDim(float64(sh) * scale)
	case None:
		dw, dh = sw, sh
	}

	noScale := dw == sw && dh == sh
	if noScale && w.opacity >= 1 {
		w.derived = nil
		return src
	}
	if w.derived != nil && w.dW == dw && w.dH == dh && w.dOp == w.opacity {
		return w.derived
	}

	out := image.NewRGBA(image.Rect(0, 0, dw, dh))
	draw.CatmullRom.Scale(out, out.Bounds(), src, src.Bounds(), draw.Src, nil)
	if w.opacity < 1 {
		applyOpacity(out, w.opacity)
	}
	w.derived = out
	w.dW = dw
	w.dH = dh
	w.dOp = w.opacity
	return out
}

func clampDim(v float64) int {
	d := max(int(v+0.5), 1)
	return d
}

func applyOpacity(img *image.RGBA, alpha float32) {
	a := uint8(alpha * 255)
	for i := 3; i < len(img.Pix); i += 4 {
		img.Pix[i] = uint8(uint32(img.Pix[i]) * uint32(a) / 255)
	}
}
