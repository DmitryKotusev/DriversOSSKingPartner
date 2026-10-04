//go:build !windows

package desktop

import "errors"

var errUnsupported = errors.New("поддерживается только в Windows")

func knownFolder(Folder) (string, error) { return "", errUnsupported }

func OwnConsole() bool { return false }

func SetTitle(string) {}

func Open(string) error { return errUnsupported }

func IsLocked(error) bool { return false }

func CreateShortcut(_, _, _, _ string) error { return errUnsupported }
