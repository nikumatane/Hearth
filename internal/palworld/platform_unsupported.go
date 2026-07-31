//go:build !windows

package palworld

import (
	"errors"
	"runtime"
	"time"
)

type nativePlatform struct{}

func (nativePlatform) sample(_, _ string) (processSample, hostSample, error) {
	return processSample{}, hostSample{}, errors.New("Palworld production adapter requires Windows")
}

func (nativePlatform) startDetached(_, _ string, _ []string, _ string) error {
	return errors.New("Palworld production adapter requires Windows")
}

func (nativePlatform) terminate(_ uint32, _ time.Time) error {
	return errors.New("Palworld production adapter requires Windows")
}

func platformSupported() error {
	return errors.New("Palworld production adapter requires Windows; current OS is " + runtime.GOOS)
}
