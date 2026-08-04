//go:build windows

package gamemanager

import (
	"math"
	"path/filepath"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"hearth/internal/panel"
)

type memoryStatusEx struct {
	Length            uint32
	MemoryLoad        uint32
	TotalPhysical     uint64
	AvailablePhysical uint64
	TotalPageFile     uint64
	AvailablePageFile uint64
	TotalVirtual      uint64
	AvailableVirtual  uint64
	AvailableExtended uint64
}

var (
	kernel32Host      = syscall.NewLazyDLL("kernel32.dll")
	procMemoryStatus  = kernel32Host.NewProc("GlobalMemoryStatusEx")
	procDiskFreeSpace = kernel32Host.NewProc("GetDiskFreeSpaceExW")
	procSystemTimes   = kernel32Host.NewProc("GetSystemTimes")
)

type hostMonitor struct {
	mu            sync.Mutex
	lastIdle      uint64
	lastKernel    uint64
	lastUser      uint64
	cpuHistory    []panel.MetricPoint
	memoryHistory []panel.MetricPoint
}

func hostUsageOrEmpty(monitor *hostMonitor, diskPath string) panel.ResourceUsage {
	memory := memoryStatusEx{Length: uint32(unsafe.Sizeof(memoryStatusEx{}))}
	result, _, _ := procMemoryStatus.Call(uintptr(unsafe.Pointer(&memory)))
	if result == 0 {
		return emptyHostUsage()
	}
	if diskPath == "" {
		diskPath = `C:\`
	}
	diskPath = filepath.VolumeName(diskPath) + `\`
	pathPointer, err := syscall.UTF16PtrFromString(diskPath)
	if err != nil {
		return emptyHostUsage()
	}
	var available, total, free uint64
	result, _, _ = procDiskFreeSpace.Call(
		uintptr(unsafe.Pointer(pathPointer)),
		uintptr(unsafe.Pointer(&available)),
		uintptr(unsafe.Pointer(&total)),
		uintptr(unsafe.Pointer(&free)),
	)
	if result == 0 {
		return emptyHostUsage()
	}
	var idle, kernel, user syscall.Filetime
	result, _, _ = procSystemTimes.Call(
		uintptr(unsafe.Pointer(&idle)), uintptr(unsafe.Pointer(&kernel)), uintptr(unsafe.Pointer(&user)),
	)
	if result == 0 {
		return emptyHostUsage()
	}

	idleValue := filetimeValue(idle)
	kernelValue := filetimeValue(kernel)
	userValue := filetimeValue(user)
	monitor.mu.Lock()
	cpu := 0.0
	if monitor.lastKernel != 0 || monitor.lastUser != 0 {
		totalDelta := kernelValue - monitor.lastKernel + userValue - monitor.lastUser
		idleDelta := idleValue - monitor.lastIdle
		if totalDelta > 0 && totalDelta >= idleDelta {
			cpu = float64(totalDelta-idleDelta) / float64(totalDelta) * 100
		}
	}
	monitor.lastIdle, monitor.lastKernel, monitor.lastUser = idleValue, kernelValue, userValue
	usedMemory := memory.TotalPhysical - memory.AvailablePhysical
	memoryPercent := percentage(usedMemory, memory.TotalPhysical)
	now := time.Now()
	monitor.cpuHistory = appendHostMetric(monitor.cpuHistory, cpu, now)
	monitor.memoryHistory = appendHostMetric(monitor.memoryHistory, memoryPercent, now)
	usage := panel.ResourceUsage{
		CPUPercent:    cpu,
		MemoryPercent: memoryPercent,
		MemoryUsedGB:  gibibytes(usedMemory), MemoryTotalGB: gibibytes(memory.TotalPhysical),
		DiskPercent: percentage(total-available, total),
		DiskUsedGB:  gibibytes(total - available), DiskTotalGB: gibibytes(total),
		CPUHistory:    append([]panel.MetricPoint(nil), monitor.cpuHistory...),
		MemoryHistory: append([]panel.MetricPoint(nil), monitor.memoryHistory...),
	}
	monitor.mu.Unlock()
	return usage
}

func emptyHostUsage() panel.ResourceUsage {
	return panel.ResourceUsage{CPUHistory: []panel.MetricPoint{}, MemoryHistory: []panel.MetricPoint{}}
}

func filetimeValue(value syscall.Filetime) uint64 {
	return uint64(value.HighDateTime)<<32 | uint64(value.LowDateTime)
}

func percentage(value, total uint64) float64 {
	if total == 0 {
		return 0
	}
	return math.Min(100, float64(value)/float64(total)*100)
}

func gibibytes(value uint64) float64 {
	return float64(value) / (1 << 30)
}

func appendHostMetric(history []panel.MetricPoint, value float64, at time.Time) []panel.MetricPoint {
	history = append(history, panel.MetricPoint{At: at, Value: value})
	if len(history) > 36 {
		history = history[len(history)-36:]
	}
	return history
}
