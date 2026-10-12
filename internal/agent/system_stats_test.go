package agent

import "testing"

func TestParseMemoryInfo(t *testing.T) {
	total, used := parseMemoryInfo("MemTotal:       16384000 kB\nMemAvailable:   4096000 kB\n")
	if total != 16384000*1024 || used != 12288000*1024 {
		t.Fatalf("unexpected memory values: total=%d used=%d", total, used)
	}
}

func TestParseLoadAverage(t *testing.T) {
	if load := parseLoadAverage("0.42 0.35 0.28 1/233 42\n"); load != 0.42 {
		t.Fatalf("unexpected load average: %v", load)
	}
}
