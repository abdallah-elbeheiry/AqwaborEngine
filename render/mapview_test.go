package render

import (
	"testing"

	"github.com/abdallah-elbeheiry/AqwaborEngine/ui"
	"github.com/gogpu/ui/uitest"
)

// --- Widget: composes with Row / Column and lays out ---

func TestMapViewComposes(t *testing.T) {
	app, err := ui.New(ui.Config{Title: "t", W: 200, H: 200})
	if err != nil {
		t.Fatal(err)
	}
	asset, err := app.Images().Load("../examples/fox.png")
	if err != nil {
		t.Fatal(err)
	}

	mv := MapView(asset)
	row := ui.Row(mv)
	uitest.LayoutWidget(row, 800, 600)
	if mv.Bounds().IsEmpty() {
		t.Fatal("MapView has empty bounds after layout")
	}

	col := ui.Column(mv)
	uitest.LayoutWidget(col, 800, 600)
	if mv.Bounds().IsEmpty() {
		t.Fatal("MapView has empty bounds after column layout")
	}
}

// --- Widget: draws the map image within its bounds ---

func TestMapViewDraws(t *testing.T) {
	app, err := ui.New(ui.Config{Title: "t", W: 200, H: 200})
	if err != nil {
		t.Fatal(err)
	}
	asset, err := app.Images().Load("../examples/fox.png")
	if err != nil {
		t.Fatal(err)
	}

	mv := MapView(asset)
	uitest.LayoutWidget(mv, 400, 300)
	canvas := uitest.DrawWidgetWithContext(mv, uitest.NewMockContext())
	if len(canvas.Images) == 0 {
		t.Fatal("expected the map image to be drawn")
	}
}
