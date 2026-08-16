package system

import (
	"bufio"
	"os"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"
)

type Metrics struct {
	CPUCount    int       `json:"cpuCount"`
	MemoryTotal uint64    `json:"memoryTotal"`
	MemoryFree  uint64    `json:"memoryFree"`
	DiskTotal   uint64    `json:"diskTotal"`
	DiskFree    uint64    `json:"diskFree"`
	Load        []float64 `json:"load"`
	Uptime      uint64    `json:"uptime"`
}

func Collect(path string) Metrics {
	m := Metrics{CPUCount: runtime.NumCPU(), Load: []float64{0, 0, 0}}
	if file, err := os.Open("/proc/meminfo"); err == nil {
		s := bufio.NewScanner(file)
		values := map[string]uint64{}
		for s.Scan() {
			fields := strings.Fields(s.Text())
			if len(fields) >= 2 {
				if n, e := strconv.ParseUint(fields[1], 10, 64); e == nil {
					values[strings.TrimSuffix(fields[0], ":")] = n * 1024
				}
			}
		}
		_ = file.Close()
		m.MemoryTotal = values["MemTotal"]
		m.MemoryFree = values["MemAvailable"]
	}
	if body, err := os.ReadFile("/proc/loadavg"); err == nil {
		fields := strings.Fields(string(body))
		for i := 0; i < 3 && i < len(fields); i++ {
			m.Load[i], _ = strconv.ParseFloat(fields[i], 64)
		}
	}
	if body, err := os.ReadFile("/proc/uptime"); err == nil {
		fields := strings.Fields(string(body))
		if len(fields) > 0 {
			seconds, _ := strconv.ParseFloat(fields[0], 64)
			m.Uptime = uint64(seconds)
		}
	}
	var stat syscall.Statfs_t
	if syscall.Statfs(path, &stat) == nil {
		m.DiskTotal = stat.Blocks * uint64(stat.Bsize)
		m.DiskFree = stat.Bavail * uint64(stat.Bsize)
	}
	return m
}

func NowUnix() int64 { return time.Now().Unix() }
