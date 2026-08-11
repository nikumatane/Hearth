//go:build !windows

package gamemanager

func discoverWindowsDSTRoots() []string {
	return nil
}
