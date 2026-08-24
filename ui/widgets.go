package ui

import (
	uiprim "github.com/gogpu/ui/primitives"
	"github.com/gogpu/ui/widget"
)

// Widget is the gogpu/ui widget interface, re-exported so engine code never
// imports gogpu/ui/widget directly.
type Widget = widget.Widget

// LabelWidget is the builder returned by Label; it supports chaining methods
// such as FontSize and Bold.
type LabelWidget = *uiprim.TextWidget

// BoxWidget is the builder returned by Box/Column/Row; it supports chaining
// methods such as Padding, Gap, Background and Rounded.
type BoxWidget = *uiprim.BoxWidget

// Label creates a text label. Chain .FontSize(n), .Bold(), .Color(c), etc.
func Label(text string) LabelWidget {
	return uiprim.Text(text)
}

// CrossAxisAlignment selects how a box aligns its children on the cross axis.
// For a Column the cross axis is horizontal; for a Row it is vertical.
type CrossAxisAlignment = uiprim.CrossAxisAlignment

const (
	// CrossStart aligns children to the start (left for Column, top for Row).
	CrossStart = uiprim.CrossAxisStart
	// CrossCenter centers children on the cross axis.
	CrossCenter = uiprim.CrossAxisCenter
	// CrossEnd aligns children to the end (right for Column, bottom for Row).
	CrossEnd = uiprim.CrossAxisEnd
	// CrossStretch stretches children to fill the cross axis (default).
	CrossStretch = uiprim.CrossAxisStretch
)

// TextAlign selects horizontal text alignment within a label.
type TextAlign = widget.TextAlign

const (
	// AlignLeft aligns text to the left (default).
	AlignLeft = widget.TextAlignLeft
	// AlignCenter centers text horizontally.
	AlignCenter = widget.TextAlignCenter
	// AlignRight aligns text to the right.
	AlignRight = widget.TextAlignRight
)

// CenterX centers a box's children on the cross axis (horizontal for a Column,
// vertical for a Row). Returns w so calls can keep chaining.
func CenterX(w BoxWidget) BoxWidget {
	return w.CrossAlign(CrossCenter)
}

// Align sets a box's cross-axis alignment, letting you choose how children are
// positioned instead of hardcoding a center. Pick CrossStart (left for a
// Column, top for a Row), CrossCenter, CrossEnd, or CrossStretch.
//
// Note: the underlying gogpu/ui BoxWidget only supports cross-axis alignment;
// main-axis (vertical for a Column) alignment is start-only in this version.
func Align(w BoxWidget, cross CrossAxisAlignment) BoxWidget {
	return w.CrossAlign(cross)
}

// CenterText centers a label's text horizontally. Returns l so calls can keep
// chaining.
func CenterText(l LabelWidget) LabelWidget {
	return l.Align(AlignCenter)
}

// Button creates a clickable button painted with the engine brand theme's
// primary/on-primary colors. For an app-specific theme, use App.Button.
func Button(text string, onClick func()) Widget {
	return newThemedButton(text, onClick, defaultM3Theme())
}

// Box lays out children in a vertical column by default.
func Box(children ...Widget) BoxWidget {
	return uiprim.Box(children...)
}

// Column lays out children top-to-bottom.
func Column(children ...Widget) BoxWidget {
	return uiprim.VBox(children...)
}

// Row lays out children left-to-right.
func Row(children ...Widget) BoxWidget {
	return uiprim.HBox(children...)
}
