package ui

import (
	"github.com/gogpu/ui/theme"
	"github.com/gogpu/ui/theme/material3"
	"github.com/gogpu/ui/widget"
)

// Theme is a plain, editable color scheme. Fill the roles you care about and
// apply it with app.SetTheme. Because widgets capture their colors when the
// tree is built, re-call app.SetTheme — and rebuild the root (or the subtree)
// — after mutating fields to reskin a live UI.
type Theme struct {
	Primary     widget.Color
	OnPrimary   widget.Color
	Secondary   widget.Color
	OnSecondary widget.Color
	Background  widget.Color
	Surface     widget.Color
	OnSurface   widget.Color
	Error       widget.Color
	OnError     widget.Color

	// Dark selects the dark color scheme (affects default shadows/mode). Set it
	// when you want a dark look, typically together with dark Background/Surface.
	Dark bool
}

// Color helpers for building widget.Color values without importing gogpu/ui.
func Hex(hex uint32) widget.Color          { return widget.Hex(hex) }
func RGB(r, g, b float32) widget.Color     { return widget.RGB(r, g, b) }
func RGBA(r, g, b, a float32) widget.Color { return widget.RGBA(r, g, b, a) }

// toGogpu converts the editable Theme into the *theme.Theme the toolkit consumes.
func (t *Theme) toGogpu() *theme.Theme {
	base := material3.New(widget.Hex(0x6750A4)).AsTheme()
	c := base.Colors
	c.Primary = t.Primary
	c.OnPrimary = t.OnPrimary
	c.Secondary = t.Secondary
	c.OnSecondary = t.OnSecondary
	c.Background = t.Background
	c.Surface = t.Surface
	c.OnSurface = t.OnSurface
	c.Error = t.Error
	c.OnError = t.OnError
	base.Colors = c
	if t.Dark {
		base.Mode = theme.ModeDark
		base.Shadows = theme.DefaultShadowsDark()
	}
	return base
}

// Six ready-made themes. Each is a plain *Theme you can also tweak, and you can
// build your own from scratch with &ui.Theme{...}.
var (
	// LightPurple — brand light purple.
	LightPurple = &Theme{
		Primary: widget.Hex(0x6750A4), OnPrimary: widget.Hex(0xFFFFFFFF),
		Secondary: widget.Hex(0x9A7BD0), OnSecondary: widget.Hex(0xFFFFFFFF),
		Background: widget.Hex(0xF6F2FA), Surface: widget.Hex(0xFFFFFFFF),
		OnSurface: widget.Hex(0x2A2433), Error: widget.Hex(0xB00020), OnError: widget.Hex(0xFFFFFFFF),
	}

	// DarkPurple — brand dark purple.
	DarkPurple = &Theme{
		Primary: widget.Hex(0xBB86FC), OnPrimary: widget.Hex(0x1B1622),
		Secondary: widget.Hex(0x9A7BD0), OnSecondary: widget.Hex(0x1B1622),
		Background: widget.Hex(0x1B1622), Surface: widget.Hex(0x251E30),
		OnSurface: widget.Hex(0xE6E1F0), Error: widget.Hex(0xCF6679), OnError: widget.Hex(0x1B1622),
		Dark: true,
	}

	// Light — neutral light (gray) theme.
	Light = &Theme{
		Primary: widget.Hex(0x37474F), OnPrimary: widget.Hex(0xFFFFFFFF),
		Secondary: widget.Hex(0x607D8B), OnSecondary: widget.Hex(0xFFFFFFFF),
		Background: widget.Hex(0xFFFFFF), Surface: widget.Hex(0xF5F5F5),
		OnSurface: widget.Hex(0x101010), Error: widget.Hex(0xB00020), OnError: widget.Hex(0xFFFFFFFF),
	}

	// Dark — neutral dark (gray) theme.
	Dark = &Theme{
		Primary: widget.Hex(0xB0BEC5), OnPrimary: widget.Hex(0x121212),
		Secondary: widget.Hex(0x78909C), OnSecondary: widget.Hex(0x121212),
		Background: widget.Hex(0x121212), Surface: widget.Hex(0x1E1E1E),
		OnSurface: widget.Hex(0xE0E0E0), Error: widget.Hex(0xCF6679), OnError: widget.Hex(0x121212),
		Dark: true,
	}

	// LightBlue — light blue theme.
	LightBlue = &Theme{
		Primary: widget.Hex(0x2196F3), OnPrimary: widget.Hex(0xFFFFFFFF),
		Secondary: widget.Hex(0x64B5F6), OnSecondary: widget.Hex(0xFFFFFFFF),
		Background: widget.Hex(0xEAF2FF), Surface: widget.Hex(0xFFFFFFFF),
		OnSurface: widget.Hex(0x0E1A2B), Error: widget.Hex(0xB00020), OnError: widget.Hex(0xFFFFFFFF),
	}

	// DarkBlue — dark blue theme.
	DarkBlue = &Theme{
		Primary: widget.Hex(0x448AFF), OnPrimary: widget.Hex(0x0A0F1E),
		Secondary: widget.Hex(0x2979FF), OnSecondary: widget.Hex(0x0A0F1E),
		Background: widget.Hex(0x0A0F1E), Surface: widget.Hex(0x121A2E),
		OnSurface: widget.Hex(0xDCE6FF), Error: widget.Hex(0xCF6679), OnError: widget.Hex(0x0A0F1E),
		Dark: true,
	}
)

// defaultM3Theme returns the engine default theme (light purple).
func defaultM3Theme() *Theme {
	return LightPurple
}

// Primary Color accessors for the most useful theme roles. Each returns a widget.Color
// ready to pass to .Background(...) / .Color(...) on a widget builder, e.g.
//
//	ui.Column(...).Background(ui.SurfaceColor(app.Theme()))
//	ui.Label("x").Color(ui.OnSurfaceColor(app.Theme()))
func Primary(t *Theme) widget.Color         { return t.Primary }
func OnPrimary(t *Theme) widget.Color       { return t.OnPrimary }
func BackgroundColor(t *Theme) widget.Color { return t.Background }
func SurfaceColor(t *Theme) widget.Color    { return t.Surface }
func OnSurfaceColor(t *Theme) widget.Color  { return t.OnSurface }
