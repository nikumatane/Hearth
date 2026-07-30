//go:build !windows

package palworld

import "os"

func replaceFile(source, destination string) error {
	return os.Rename(source, destination)
}
