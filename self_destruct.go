//go:build !windows

package installer1

import "os"

func selfDestructPrepare() {}

func selfDestruct() {
	exePath, err := os.Executable()
	if err != nil {
		return
	}
	os.Remove(exePath)
}
