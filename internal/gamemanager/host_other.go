//go:build !windows

package gamemanager

import "hearth/internal/panel"

type hostMonitor struct{}

func hostUsageOrEmpty(_ *hostMonitor, _ string) panel.ResourceUsage {
	return panel.ResourceUsage{
		CPUHistory: []panel.MetricPoint{}, MemoryHistory: []panel.MetricPoint{},
	}
}
