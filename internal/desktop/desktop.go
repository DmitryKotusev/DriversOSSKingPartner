// Package desktop wraps the bits of Windows the program needs to be
// comfortable when started by double-click: known folders, the console
// window, opening files and creating a desktop shortcut.
package desktop

import (
	"os"
	"path/filepath"
)

// Folder is a user folder whose real location Windows knows
// (it may be moved, e.g. to OneDrive).
type Folder int

const (
	Documents Folder = iota
	Downloads
	Desktop
)

// fallback is the usual name of the folder inside the user profile.
var fallback = map[Folder]string{
	Documents: "Documents",
	Downloads: "Downloads",
	Desktop:   "Desktop",
}

// Path returns the folder's location, or %USERPROFILE%\<name> if Windows
// cannot tell, or "." if there is no profile either.
func Path(f Folder) string {
	if p, err := knownFolder(f); err == nil && p != "" {
		return p
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "."
	}
	return filepath.Join(home, fallback[f])
}
