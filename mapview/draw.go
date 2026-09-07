// Package mapview provides a pannable/zoomable image widget for displaying
// large maps inside a viewport. It owns a camera.Camera and uses CPU scaling
// to draw the visible sub-region of the source image.
//
// This is a UI widget — it does not touch wgpu or the GPU render pipeline.
// For GPU-accelerated map rendering (vector data, strokes), see maprender.
package mapview

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
	tlx, tly := cam.LocalToWorld(0, 0, vp.Width, vp.Height)
	brx, bry := cam.LocalToWorld(vp.Width, vp.Height, vp.Width, vp.Height)
	x0 := clampF(tlx, 0, float32(sw))
	y0 := clampF(tly, 0, float32(sh))
	x1 := clampF(brx, 0, float32(sw))
	y1 := clampF(bry, 0, float32(sh))
	if x1 <= x0 || y1 <= y0 {
		return
	}

	sub := image.Rect(int(x0+0.5), int(y0+0.5), int(x1+0.5), int(y1+0.5))
	subW, subH := sub.Dx(), sub.Dy()
	if subW <= 0 || subH <= 0 {
		return
	}

	dstX := (x0 - tlx) * cam.Zoom
	dstY := (y0 - tly) * cam.Zoom
	dstW := int(float32(subW)*cam.Zoom + 0.5)
	dstH := int(float32(subH)*cam.Zoom + 0.5)

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
