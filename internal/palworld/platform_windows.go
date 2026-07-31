//go:build windows

package palworld

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

const (
	th32csSnapProcess             = 0x00000002
	processTerminate              = 0x0001
	processVMRead                 = 0x0010
	processQueryLimitedInfo       = 0x1000
	detachedProcess               = 0x00000008
	createNewProcessGroup         = 0x00000200
	windowsToUnixEpoch100NS int64 = 116444736000000000
)

type processEntry32 struct {
	Size              uint32
	Usage             uint32
	ProcessID         uint32
	DefaultHeapID     uintptr
	ModuleID          uint32
	Threads           uint32
	ParentProcessID   uint32
	PriorityClassBase int32
	Flags             uint32
	ExeFile           [syscall.MAX_PATH]uint16
}

type processMemoryCounters struct {
	Size                     uint32
	PageFaultCount           uint32
	PeakWorkingSetSize       uintptr
	WorkingSetSize           uintptr
	QuotaPeakPagedPoolUsage  uintptr
	QuotaPagedPoolUsage      uintptr
	QuotaPeakNonPagedPoolUse uintptr
	QuotaNonPagedPoolUsage   uintptr
	PagefileUsage            uintptr
	PeakPagefileUsage        uintptr
}

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
	kernel32Platform         = syscall.NewLazyDLL("kernel32.dll")
	psapiPlatform            = syscall.NewLazyDLL("psapi.dll")
	procCreateSnapshot       = kernel32Platform.NewProc("CreateToolhelp32Snapshot")
	procProcessFirst         = kernel32Platform.NewProc("Process32FirstW")
	procProcessNext          = kernel32Platform.NewProc("Process32NextW")
	procOpenProcess          = kernel32Platform.NewProc("OpenProcess")
	procGetProcessTimes      = kernel32Platform.NewProc("GetProcessTimes")
	procTerminateProcess     = kernel32Platform.NewProc("TerminateProcess")
	procGetProcessMemoryInfo = psapiPlatform.NewProc("GetProcessMemoryInfo")
	procGlobalMemoryStatus   = kernel32Platform.NewProc("GlobalMemoryStatusEx")
	procGetDiskFreeSpace     = kernel32Platform.NewProc("GetDiskFreeSpaceExW")
	procGetSystemTimes       = kernel32Platform.NewProc("GetSystemTimes")
)

type nativePlatform struct{}

func platformSupported() error {
	return nil
}

func (nativePlatform) sample(processName, diskPath string) (processSample, hostSample, error) {
	host, err := sampleHost(diskPath)
	if err != nil {
		return processSample{}, hostSample{}, err
	}
	process, err := sampleProcess(processName)
	if err != nil {
		return processSample{}, host, err
	}
	return process, host, nil
}

func sampleProcess(processName string) (processSample, error) {
	handle, _, callErr := procCreateSnapshot.Call(th32csSnapProcess, 0)
	if handle == uintptr(syscall.InvalidHandle) {
		return processSample{}, callErr
	}
	defer syscall.CloseHandle(syscall.Handle(handle))

	entry := processEntry32{Size: uint32(unsafe.Sizeof(processEntry32{}))}
	result, _, callErr := procProcessFirst.Call(handle, uintptr(unsafe.Pointer(&entry)))
	if result == 0 {
		if errors.Is(callErr, syscall.ERROR_NO_MORE_FILES) {
			return processSample{}, nil
		}
		return processSample{}, callErr
	}
	for {
		if strings.EqualFold(syscall.UTF16ToString(entry.ExeFile[:]), processName) {
			return inspectProcess(entry.ProcessID)
		}
		result, _, callErr = procProcessNext.Call(handle, uintptr(unsafe.Pointer(&entry)))
		if result == 0 {
			if errors.Is(callErr, syscall.ERROR_NO_MORE_FILES) {
				return processSample{}, nil
			}
			return processSample{}, callErr
		}
	}
}

func inspectProcess(processID uint32) (processSample, error) {
	handle, _, callErr := procOpenProcess.Call(processQueryLimitedInfo|processVMRead, 0, uintptr(processID))
	if handle == 0 {
		return processSample{}, callErr
	}
	defer syscall.CloseHandle(syscall.Handle(handle))

	memory := processMemoryCounters{Size: uint32(unsafe.Sizeof(processMemoryCounters{}))}
	result, _, callErr := procGetProcessMemoryInfo.Call(
		handle, uintptr(unsafe.Pointer(&memory)), uintptr(memory.Size),
	)
	if result == 0 {
		return processSample{}, callErr
	}
	var created, exited, kernel, user syscall.Filetime
	result, _, callErr = procGetProcessTimes.Call(
		handle,
		uintptr(unsafe.Pointer(&created)),
		uintptr(unsafe.Pointer(&exited)),
		uintptr(unsafe.Pointer(&kernel)),
		uintptr(unsafe.Pointer(&user)),
	)
	if result == 0 {
		return processSample{}, callErr
	}
	return processSample{
		Running: true, PID: processID, MemoryBytes: uint64(memory.WorkingSetSize),
		CPU100NS:  filetimeValue(kernel) + filetimeValue(user),
		StartedAt: filetimeTime(created),
	}, nil
}

func sampleHost(diskPath string) (hostSample, error) {
	memory := memoryStatusEx{Length: uint32(unsafe.Sizeof(memoryStatusEx{}))}
	result, _, callErr := procGlobalMemoryStatus.Call(uintptr(unsafe.Pointer(&memory)))
	if result == 0 {
		return hostSample{}, callErr
	}

	pathPointer, err := syscall.UTF16PtrFromString(diskPath)
	if err != nil {
		return hostSample{}, err
	}
	var available, total, free uint64
	result, _, callErr = procGetDiskFreeSpace.Call(
		uintptr(unsafe.Pointer(pathPointer)),
		uintptr(unsafe.Pointer(&available)),
		uintptr(unsafe.Pointer(&total)),
		uintptr(unsafe.Pointer(&free)),
	)
	if result == 0 {
		return hostSample{}, callErr
	}

	var idle, kernel, user syscall.Filetime
	result, _, callErr = procGetSystemTimes.Call(
		uintptr(unsafe.Pointer(&idle)),
		uintptr(unsafe.Pointer(&kernel)),
		uintptr(unsafe.Pointer(&user)),
	)
	if result == 0 {
		return hostSample{}, callErr
	}
	return hostSample{
		Idle100NS: filetimeValue(idle), Kernel100NS: filetimeValue(kernel), User100NS: filetimeValue(user),
		MemoryTotal: memory.TotalPhysical, MemoryAvailable: memory.AvailablePhysical,
		DiskTotal: total, DiskAvailable: available,
	}, nil
}

func filetimeValue(value syscall.Filetime) uint64 {
	return uint64(value.HighDateTime)<<32 | uint64(value.LowDateTime)
}

func filetimeTime(value syscall.Filetime) time.Time {
	ticks := int64(filetimeValue(value))
	if ticks <= windowsToUnixEpoch100NS {
		return time.Time{}
	}
	return time.Unix(0, (ticks-windowsToUnixEpoch100NS)*100)
}

func (nativePlatform) startDetached(executable, workingDirectory string, arguments []string, logPath string) error {
	logFile, err := os.OpenFile(logPath, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	defer logFile.Close()

	command := exec.Command(executable, arguments...)
	command.Dir = workingDirectory
	command.Stdin = nil
	command.Stdout = logFile
	command.Stderr = logFile
	command.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: detachedProcess | createNewProcessGroup,
		HideWindow:    true,
	}
	if err := command.Start(); err != nil {
		return err
	}
	return command.Process.Release()
}

func (nativePlatform) terminate(processID uint32, expectedStartedAt time.Time) error {
	if expectedStartedAt.IsZero() {
		return errors.New("expected process start time is required")
	}
	handle, _, callErr := procOpenProcess.Call(
		processTerminate|processQueryLimitedInfo,
		0,
		uintptr(processID),
	)
	if handle == 0 {
		return callErr
	}
	defer syscall.CloseHandle(syscall.Handle(handle))

	var created, exited, kernel, user syscall.Filetime
	result, _, callErr := procGetProcessTimes.Call(
		handle,
		uintptr(unsafe.Pointer(&created)),
		uintptr(unsafe.Pointer(&exited)),
		uintptr(unsafe.Pointer(&kernel)),
		uintptr(unsafe.Pointer(&user)),
	)
	if result == 0 {
		return callErr
	}
	actualStartedAt := filetimeTime(created)
	if actualStartedAt.IsZero() || !actualStartedAt.Equal(expectedStartedAt) {
		return fmt.Errorf(
			"process PID %d start time changed from %s to %s",
			processID,
			expectedStartedAt.Format(time.RFC3339Nano),
			actualStartedAt.Format(time.RFC3339Nano),
		)
	}

	result, _, callErr = procTerminateProcess.Call(handle, 1)
	if result == 0 {
		return callErr
	}
	return nil
}
