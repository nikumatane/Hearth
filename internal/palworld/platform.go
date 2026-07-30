package palworld

import "time"

type processSample struct {
	Running     bool
	PID         uint32
	MemoryBytes uint64
	CPU100NS    uint64
	StartedAt   time.Time
}

type hostSample struct {
	Idle100NS       uint64
	Kernel100NS     uint64
	User100NS       uint64
	MemoryTotal     uint64
	MemoryAvailable uint64
	DiskTotal       uint64
	DiskAvailable   uint64
}

type platformAdapter interface {
	sample(processName, diskPath string) (processSample, hostSample, error)
	startDetached(executable, workingDirectory string, arguments []string, logPath string) error
}
