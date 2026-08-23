package examples

import (
	"fmt"

	"github.com/abdallah-elbeheiry/AqwaborEngine/input"
	"github.com/abdallah-elbeheiry/AqwaborEngine/input/backend/headless"
)

// Example_press demonstrates a basic press/release Action.
func Example_press() {
	backend := headless.New()
	in := input.NewManager(backend)

	jump := in.Action("jump")
	in.BindKey(jump, input.KeySpace)
	var jumps int
	jump.OnPressed(func(input.Context) { jumps++ })

	backend.KeyDown(input.KeySpace)
	in.Update(0)
	backend.KeyUp(input.KeySpace)
	in.Update(0)

	fmt.Println(jumps)
	// Output: 1
}

// Example_hold demonstrates an OnHold Action that fires while held.
func Example_hold() {
	backend := headless.New()
	in := input.NewManager(backend)

	charge := in.Action("charge")
	in.BindKey(charge, input.KeySpace)
	var charged int
	charge.OnHold(0.4, func(input.Context) { charged++ })

	backend.KeyDown(input.KeySpace)
	in.Update(0)
	in.Update(0.5) // held past the 0.4s threshold

	fmt.Println(charged)
	// Output: 1
}

// Example_toggle demonstrates an OnToggle Action.
func Example_toggle() {
	backend := headless.New()
	in := input.NewManager(backend)

	flash := in.Action("flash")
	in.BindKey(flash, input.KeyF)
	var on bool
	flash.OnToggle(func(active bool, _ input.Context) { on = active })

	backend.KeyDown(input.KeyF)
	in.Update(0)
	backend.KeyUp(input.KeyF)
	in.Update(0)

	fmt.Println(on)
	// Output: true
}

// Example_combo demonstrates a simultaneous key combo.
func Example_combo() {
	backend := headless.New()
	in := input.NewManager(backend)

	stealth := in.Combo("stealth", input.KeyX, input.KeyV)
	var hit int
	stealth.OnPressed(func(input.Context) { hit++ })

	backend.KeyDown(input.KeyX)
	in.Update(0)
	backend.KeyDown(input.KeyV)
	in.Update(0) // both keys down -> combo fires

	fmt.Println(hit)
	// Output: 1
}

// Example_mouseClick demonstrates a mouse click that reports its position.
func Example_mouseClick() {
	backend := headless.New()
	in := input.NewManager(backend)

	sel := in.Action("select")
	in.BindMouseButton(sel, input.MouseButtonLeft)
	var x, y float64
	sel.OnPressed(func(c input.Context) { x, y = c.MousePosition() })

	backend.MouseMove(50, 60)
	backend.MouseDown(input.MouseButtonLeft, 50, 60)
	in.Update(0)

	fmt.Printf("%.0f,%.0f\n", x, y)
	// Output: 50,60
}
