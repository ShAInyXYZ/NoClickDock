//go:build !windows

package main

import "path/filepath"

// documentsDir is ~/Documents, where the Python power monitor kept its log.
func documentsDir(home string) string { return filepath.Join(home, "Documents") }
