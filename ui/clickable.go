package ui

import (
	"github.com/gogpu/ui/event"
	"github.com/gogpu/ui/geometry"
	"github.com/gogpu/ui/gesture"
	"github.com/gogpu/ui/widget"
)

// Clickable wraps any widget so a click anywhere inside its bounds invokes
// onClick. It is a thin interaction layer over the toolkit's gesture pipeline
// (the same one core/button uses) and introduces no separate input system.
//
//	ui.Clickable(ui.Image(play), startGame)
//	ui.Clickable(ui.Row(ui.Image(play).Size(20, 20), ui.Label("Play")), startGame)
func Clickable(content Widget, onClick func()) Widget {
	w := &clickableWidget{
		content: content,
		onClick: onClick,
	}
	w.SetVisible(true)
	w.SetEnabled(true)
	return w
}

// clickableWidget forwards layout, drawing and child management to its wrapped
// content and reports clicks through a gesture ClickRecognizer.
type clickableWidget struct {
	widget.WidgetBase

	content  Widget
	onClick  func()
	clickRec *gesture.ClickRecognizer
}

// newClickRecognizer wires the toolkit ClickRecognizer to this widget. The
// recognizer only fires OnClick after a full press+release, so we just guard
// the left button here.
func (w *clickableWidget) newClickRecognizer() *gesture.ClickRecognizer {
	return gesture.NewClickRecognizer(gesture.ClickConfig{
		MaxClickCount: 1,
		OnClick: func(d gesture.ClickDetails) {
			if d.Button == event.ButtonLeft {
				w.fire()
			}
		},
	})
}

func (w *clickableWidget) fire() {
	if w.onClick != nil {
		w.onClick()
	}
}

// Layout sizes the clickable to its content and adopts that size. It also
// positions the content to fill the clickable (no insets) so descendant
// bounds/hit-testing are correct for children that don't self-bound (e.g. the
// toolkit's Button).
func (w *clickableWidget) Layout(ctx widget.Context, c geometry.Constraints) geometry.Size {
	if w.content == nil {
		return c.Constrain(geometry.Sz(0, 0))
	}
	s := w.content.Layout(ctx, c)
	// Position the content to fill the clickable (no insets) so descendant
	// bounds/hit-testing are correct for children that don't self-bound (e.g.
	// the toolkit's Button). SetBounds is an optional WidgetBase method.
	if bs, ok := w.content.(interface{ SetBounds(geometry.Rect) }); ok {
		bs.SetBounds(geometry.FromPointSize(w.Position(), s))
	}
	w.SetBounds(geometry.FromPointSize(w.Position(), s))
	return s
}

// Draw renders the wrapped content. It establishes the clickable's own frame
// with a transform first: containers (Box/Column) push the parent frame but do
// not offset individual children, so without this the content would paint in
// the grandparent's coordinate space (e.g. the window's top-left corner).
func (w *clickableWidget) Draw(ctx widget.Context, canvas widget.Canvas) {
	if w.content == nil {
		return
	}
	canvas.PushTransform(w.Bounds().Min)
	w.content.Draw(ctx, canvas)
	canvas.PopTransform()
}

// Event forwards input events to the wrapped content so nested interactive
// widgets keep working. Clicks themselves are detected by the gesture
// recognizer (see GestureHitTest), not here.
func (w *clickableWidget) Event(ctx widget.Context, e event.Event) bool {
	if w.content != nil {
		return w.content.Event(ctx, e)
	}
	return false
}

// Children returns the wrapped content so it is mounted/unmounted with the tree.
func (w *clickableWidget) Children() []widget.Widget {
	if w.content == nil {
		return nil
	}
	return []widget.Widget{w.content}
}

// GestureHitTest exposes the click recognizer to the toolkit gesture pipeline.
//
// To honor nested interactive content (e.g. a Button inside a Clickable) the
// clickable yields to any gesture-aware descendant that is actually hit by this
// pointer: it returns no recognizer of its own, so the descendant becomes the
// sole arena participant and the parent's onClick is never invoked. This mirrors
// the toolkit's own container pattern (see gesture.GestureAware) and keeps a
// single input pipeline. The decision is per pointer (no global state), and it
// is position-specific: a non-interactive region of the clickable still fires
// the parent.
func (w *clickableWidget) GestureHitTest(pos geometry.Point) []gesture.Recognizer {
	if w.interactiveDescendantAt(pos) {
		return nil
	}
	if w.clickRec == nil {
		w.clickRec = w.newClickRecognizer()
	}
	return []gesture.Recognizer{w.clickRec}
}

// interactiveDescendantAt reports whether pos falls inside a gesture-aware
// descendant that would itself participate in the gesture arena at that point.
// It walks the whole content subtree so suppression composes recursively
// (Clickable > Clickable > Button).
func (w *clickableWidget) interactiveDescendantAt(localPos geometry.Point) bool {
	global := localPos
	o := w.ScreenOrigin()
	global = geometry.Pt(localPos.X+o.X, localPos.Y+o.Y)
	return interactiveAtPoint(w.content, global)
}

// interactiveAtPoint reports whether global lies within a gesture-aware widget
// whose own GestureHitTest would return a recognizer there. Requiring both the
// widget's screen bounds to contain the point and its GestureHitTest to accept
// it avoids suppressing the parent merely because a nested GestureAware widget
// exists off to the side or behind a partial interactive region (e.g. a
// collapsible header).
func interactiveAtPoint(w widget.Widget, global geometry.Point) bool {
	if w == nil {
		return false
	}
	if ga, ok := w.(gesture.GestureAware); ok {
		if sb, ok := w.(interface{ ScreenBounds() geometry.Rect }); ok && sb.ScreenBounds().Contains(global) {
			local := global
			if so, ok := w.(interface{ ScreenOrigin() geometry.Point }); ok {
				o := so.ScreenOrigin()
				local = geometry.Pt(global.X-o.X, global.Y-o.Y)
			}
			if recs := ga.GestureHitTest(local); len(recs) > 0 {
				return true
			}
		}
	}
	for _, c := range w.Children() {
		if interactiveAtPoint(c, global) {
			return true
		}
	}
	return false
}

// ImageButton is a clickable image. It is a thin, unopinionated wrapper over
// Clickable + Image: the image keeps its natural/default sizing and the caller
// is free to size it explicitly (e.g. ui.Image(icon).Size(32, 32)). No fixed
// dimensions or fit mode are baked in.
//
//	ui.ImageButton(play, startGame)
//	ui.Clickable(ui.Image(play).Size(32, 32), startGame) // explicit sizing
func ImageButton(asset *ImageAsset, onClick func()) Widget {
	return Clickable(Image(asset), onClick)
}
