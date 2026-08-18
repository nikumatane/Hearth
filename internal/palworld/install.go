package palworld

import (
	"time"

	"hearth/internal/steamapp"
)

type InstallProgress func(stage string, progress int, detail string)

// InstallDedicatedServer installs Palworld into SteamCMD's standard library.
// The caller must have received explicit administrator confirmation first.
func InstallDedicatedServer(
	steamCmd string,
	logPath string,
	noProgressTimeout time.Duration,
	report InstallProgress,
) error {
	return steamapp.InstallDedicatedServer(
		steamCmd,
		logPath,
		noProgressTimeout,
		steamapp.InstallSpec{AppID: palworldAppID, ProductName: "Palworld Dedicated Server"},
		steamapp.InstallProgress(report),
	)
}

func installProgressFromSteamLog(line string) (int, bool) {
	return steamapp.InstallProgressFromSteamLog(line)
}
