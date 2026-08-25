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

// Layout sizes the clickable to its content and adopts that size.
func (w *clickableWidget) Layout(ctx widget.Context, c geometry.Constraints) geometry.Size {
	if w.content == nil {
		return c.Constrain(geometry.Sz(0, 0))
	}
	s := w.content.Layout(ctx, c)
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
func (w *clickableWidget) GestureHitTest(_ geometry.Point) []gesture.Recognizer {
	if w.clickRec == nil {
		w.clickRec = w.newClickRecognizer()
	}
	return []gesture.Recognizer{w.clickRec}
}

// ImageButton is a clickable image capped to a 128×128 box (Contain) so large
// assets don't overflow the layout. It is a thin wrapper over Clickable + Image.
//
//	ui.ImageButton(play, startGame)
func ImageButton(asset *ImageAsset, onClick func()) Widget {
	return Clickable(Image(asset).Size(128, 128).Fit(Contain), onClick)
}
