//go:build !windows

package appupdate

import "errors"

func RunUpdater(Plan) error { return errors.New("Hearth updater only runs on Windows") }
