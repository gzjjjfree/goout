// core/statusbar/statusbar_stub.go
//go:build !android

package statusbar

import (
	"fyne.io/fyne/v2"
)

// AdaptStatusBar is a stub for non-Android platforms.
func AdaptStatusBar(a fyne.App) {
	// No-op
}
