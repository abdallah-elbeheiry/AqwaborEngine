package input

// Key identifies a physical keyboard key.
//
// The numeric values intentionally match the gogpu input package
// (github.com/gogpu/gogpu/input) so the gogpu backend can pass key codes
// through without a conversion table. They are stable for a given engine
// version and are only used internally as opaque identifiers.
type Key uint16

// Keyboard key codes.
const (
	KeyUnknown Key = iota

	// KeyF1 Function keys
	KeyF1
	KeyF2
	KeyF3
	KeyF4
	KeyF5
	KeyF6
	KeyF7
	KeyF8
	KeyF9
	KeyF10
	KeyF11
	KeyF12

	// Key0 Number keys
	Key0
	Key1
	Key2
	Key3
	Key4
	Key5
	Key6
	Key7
	Key8
	Key9

	// KeyA Letter keys
	KeyA
	KeyB
	KeyC
	KeyD
	KeyE
	KeyF
	KeyG
	KeyH
	KeyI
	KeyJ
	KeyK
	KeyL
	KeyM
	KeyN
	KeyO
	KeyP
	KeyQ
	KeyR
	KeyS
	KeyT
	KeyU
	KeyV
	KeyW
	KeyX
	KeyY
	KeyZ

	// KeySpace Special keys
	KeySpace
	KeyEnter
	KeyEscape
	KeyBackspace
	KeyTab
	KeyCapsLock
	KeyShiftLeft
	KeyShiftRight
	KeyControlLeft
	KeyControlRight
	KeyAltLeft
	KeyAltRight
	KeySuperLeft  // Windows/Command key
	KeySuperRight // Windows/Command key

	// KeyUp Arrow keys
	KeyUp
	KeyDown
	KeyLeft
	KeyRight

	// KeyInsert Navigation keys
	KeyInsert
	KeyDelete
	KeyHome
	KeyEnd
	KeyPageUp
	KeyPageDown

	// KeyMinus Punctuation
	KeyMinus
	KeyEqual
	KeyLeftBracket
	KeyRightBracket
	KeyBackslash
	KeySemicolon
	KeyApostrophe
	KeyGrave
	KeyComma
	KeyPeriod
	KeySlash

	// KeyNumpad0 Numpad
	KeyNumpad0
	KeyNumpad1
	KeyNumpad2
	KeyNumpad3
	KeyNumpad4
	KeyNumpad5
	KeyNumpad6
	KeyNumpad7
	KeyNumpad8
	KeyNumpad9
	KeyNumpadAdd
	KeyNumpadSubtract
	KeyNumpadMultiply
	KeyNumpadDivide
	KeyNumpadEnter
	KeyNumpadDecimal
	KeyNumLock

	// KeyPrintScreen Other
	KeyPrintScreen
	KeyScrollLock
	KeyPause
	KeyCancel // Win32 Ctrl+Break (VK_CANCEL); distinct from KeyPause

	KeyCount // Number of keys
)

// MouseButton identifies a mouse button.
//
// The numeric values intentionally match the gogpu input package.
type MouseButton uint8

const (
	MouseButtonLeft MouseButton = iota
	MouseButtonRight
	MouseButtonMiddle
	MouseButton4
	MouseButton5
	MouseButtonCount
)
