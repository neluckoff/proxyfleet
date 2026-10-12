package agent

import (
	"os"
	"runtime"
	"strconv"
	"strings"
)

type systemStats struct {
	MemoryAllocBytes     int64
	MemorySysBytes       int64
	Goroutines           int
	HostMemoryUsedBytes  int64
	HostMemoryTotalBytes int64
	LoadOne              float64
}

func readSystemStats() systemStats {
	var memory runtime.MemStats
	runtime.ReadMemStats(&memory)
	stats := systemStats{
		MemoryAllocBytes: int64(memory.Alloc),
		MemorySysBytes:   int64(memory.Sys),
		Goroutines:       runtime.NumGoroutine(),
	}

	if data, err := os.ReadFile("/proc/meminfo"); err == nil {
		stats.HostMemoryTotalBytes, stats.HostMemoryUsedBytes = parseMemoryInfo(string(data))
	}
	if data, err := os.ReadFile("/proc/loadavg"); err == nil {
		stats.LoadOne = parseLoadAverage(string(data))
	}
	return stats
}

func parseMemoryInfo(value string) (total, used int64) {
	var available int64
	for _, line := range strings.Split(value, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		amount, err := strconv.ParseInt(fields[1], 10, 64)
		if err != nil {
			continue
		}
		switch strings.TrimSuffix(fields[0], ":") {
		case "MemTotal":
			total = amount * 1024
		case "MemAvailable":
			available = amount * 1024
		}
	}
	if total > available {
		used = total - available
	}
	return total, used
}

func parseLoadAverage(value string) float64 {
	fields := strings.Fields(value)
	if len(fields) == 0 {
		return 0
	}
	load, _ := strconv.ParseFloat(fields[0], 64)
	return load
}
