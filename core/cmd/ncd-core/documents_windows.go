package main

import "golang.org/x/sys/windows"

// documentsDir is the Documents known folder, wherever the user moved it
// (OneDrive often does).
func documentsDir(home string) string {
	if dir, err := windows.KnownFolderPath(windows.FOLDERID_Documents, 0); err == nil && dir != "" {
		return dir
	}
	return home + `\Documents`
}
