// Package render holds camera-driven canvas rendering. Its central piece is
// MapView, a UI widget that displays a large image inside a viewport and lets
// the user pan (left-drag) and zoom (mouse wheel, toward the cursor). The
// drawing itself is factored into DrawCache.Draw so any other camera-driven
// canvas (minimap, province overlay, ...) can reuse the visible-region scaling
// without re-implementing it.
//
// render is deliberately decoupled from the generic ui library: it consumes an
// ui.ImageAsset but never reaches into ui's internals beyond that public type.
package render

import (
	draw "golang.org/x/image/draw"
	"image"

	"github.com/abdallah-elbeheiry/AqwaborEngine/camera"
	"github.com/gogpu/ui/geometry"
	"github.com/gogpu/ui/widget"
)

// DrawCache draws an image through a camera onto a widget.Canvas, scaling only
// the visible sub-region on the CPU (the toolkit Canvas only supports
// translation, not scaling). The scaled output and source sub-region buffers
// are reused between frames so panning — which keeps the destination size
// constant — does not allocate every frame.
type DrawCache struct {
	derived *image.RGBA
	dW, dH  int
	tmp     *image.RGBA
	tW, tH  int
}

// Draw renders the portion of src visible through cam into vp-sized local
// pixels on canvas. It clips nothing itself; the caller is expected to have
// already clipped to the widget bounds.
func (c *DrawCache) Draw(canvas widget.Canvas, src image.Image, cam *camera.Camera, vp geometry.Size) {
	sw, sh := src.Bounds().Dx(), src.Bounds().Dy()
	if sw <= 0 || sh <= 0 {
		return
	}

	// Visible world rectangle, clamped to the image itself.
	tl := cam.LocalToWorld(geometry.Pt(0, 0), vp)
	br := cam.LocalToWorld(geometry.Pt(vp.Width, vp.Height), vp)
	x0 := clampF(tl.X, 0, float32(sw))
	y0 := clampF(tl.Y, 0, float32(sh))
	x1 := clampF(br.X, 0, float32(sw))
	y1 := clampF(br.Y, 0, float32(sh))
	if x1 <= x0 || y1 <= y0 {
		return
	}

	sub := image.Rect(int(x0+0.5), int(y0+0.5), int(x1+0.5), int(y1+0.5))
	subW, subH := sub.Dx(), sub.Dy()
	if subW <= 0 || subH <= 0 {
		return
	}

	dstX := (x0 - tl.X) * cam.Zoom()
	dstY := (y0 - tl.Y) * cam.Zoom()
	dstW := int(float32(subW)*cam.Zoom() + 0.5)
	dstH := int(float32(subH)*cam.Zoom() + 0.5)

	scaled := c.scaleRegion(src, sub, dstW, dstH)
	if scaled == nil {
		return
	}
	canvas.DrawImage(scaled, geometry.Pt(dstX, dstY))
}

// scaleRegion copies the source sub-rectangle into a reusable buffer, then
// scales it to (dw,dh), reusing the output buffer when its size is unchanged so
// panning (which keeps dw/dh constant) does not allocate every frame.
func (c *DrawCache) scaleRegion(src image.Image, sub image.Rectangle, dw, dh int) image.Image {
	if dw <= 0 || dh <= 0 {
		return nil
	}
	subW, subH := sub.Dx(), sub.Dy()

	if c.tmp == nil || c.tW != subW || c.tH != subH {
		c.tmp = image.NewRGBA(image.Rect(0, 0, subW, subH))
		c.tW, c.tH = subW, subH
	}
	draw.Draw(c.tmp, c.tmp.Bounds(), src, sub.Min, draw.Src)

	if subW == dw && subH == dh {
		return c.tmp
	}

	if c.derived == nil || c.dW != dw || c.dH != dh {
		c.derived = image.NewRGBA(image.Rect(0, 0, dw, dh))
		c.dW, c.dH = dw, dh
	}
	draw.CatmullRom.Scale(c.derived, c.derived.Bounds(), c.tmp, c.tmp.Bounds(), draw.Src, nil)
	return c.derived
}

func clampF(v, lo, hi float32) float32 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
