package sessions

import (
	"os"
	"path/filepath"
)

// UnsupportedReasonixStores reports native stores that this reader cannot
// decode. It inspects file names only: it never opens a writer, migrates a
// session, or treats a cache, lock, or sidecar as a conversation.
func UnsupportedReasonixStores() int {
	root := ReasonixDir()
	patterns := []string{
		filepath.Join(root, "sessions-v4", "*"),
		filepath.Join(root, "projects", "*", "sessions-v4", "*"),
		filepath.Join(root, "desktop-sessions-v5", "by-id", "*"),
	}
	seen := map[string]bool{}
	for _, pattern := range patterns {
		dirs, _ := SessionGlob(pattern)
		for _, dir := range dirs {
			manifest, err := os.Stat(filepath.Join(dir, "manifest.json"))
			if err != nil || !manifest.Mode().IsRegular() {
				continue
			}
			frames, err := os.Stat(filepath.Join(dir, "events.frames"))
			if err == nil && frames.Mode().IsRegular() {
				seen[dir] = true
			}
		}
	}
	return len(seen)
}
