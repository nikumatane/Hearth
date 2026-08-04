//go:build !windows

package appupdate

import "errors"

func launchUpdateHelper(string) error {
	return errors.New("panel update installation is only supported on Windows")
}
