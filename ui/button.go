package ui

import (
	"github.com/gogpu/ui/core/button"
	"github.com/gogpu/ui/widget"
)

// themedPainter renders a button with the active theme's primary background and
// on-primary foreground, including hover/press feedback. The core/button
// DefaultPainter ignores the app theme (it hardcodes a grey background and
// black text), so the façade supplies its own painter to make themes visible.
type themedPainter struct {
	button.DefaultPainter
	primary   widget.Color
	onPrimary widget.Color
}

func (p themedPainter) PaintButton(canvas widget.Canvas, state button.PaintState) {
	if state.Bounds.IsEmpty() {
		return
	}
	radius := float32(8)
	if state.Radius != nil {
		radius = *state.Radius
	}

	bg := p.primary
	if state.Background != nil {
		bg = *state.Background
	}
	if state.Disabled {
		bg = bg.Lerp(widget.ColorWhite, 0.6)
	} else {
		bg = stateModifier(bg, state.Hovered, state.Pressed)
	}
	canvas.DrawRoundRect(state.Bounds, bg, radius)

	fg := p.onPrimary
	if state.Disabled {
		fg = fg.WithAlpha(0.6)
	}
	fontSize := p.ButtonFontSize(state.Size)
	canvas.DrawText(state.Text, state.Bounds, fontSize, fg, false, widget.TextAlignCenter)

	if state.Focused && !state.Disabled {
		ring := state.Bounds.Expand(2)
		canvas.StrokeRoundRect(ring, focusRingColor(), radius+2, 2)
	}
}

// stateModifier darkens on press and lightens on hover, matching the feel of
// the toolkit's built-in button states.
func stateModifier(base widget.Color, hovered, pressed bool) widget.Color {
	if pressed {
		return base.Lerp(widget.ColorBlack, 0.15)
	}
	if hovered {
		return base.Lerp(widget.ColorWhite, 0.1)
	}
	return base
}

func focusRingColor() widget.Color {
	return widget.Hex(0x6750A4).WithAlpha(0.7)
}

// newThemedButton builds a button painted with the given theme's colors.
func newThemedButton(text string, onClick func(), t *Theme) Widget {
	return button.New(
		button.TextOpt(text),
		button.OnClick(onClick),
		button.PainterOpt(themedPainter{
			primary:   t.Primary,
			onPrimary: t.OnPrimary,
		}),
	)
}
