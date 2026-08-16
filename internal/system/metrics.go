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

// Metrics is collected only when the dashboard requests it. No background
// sampler is kept alive, which keeps KotaUI suitable for small VPS instances.
type Metrics struct {
	CPUCount        int       `json:"cpuCount"`
	CPUPercent      float64   `json:"cpuPercent"`
	MemoryTotal     uint64    `json:"memoryTotal"`
	MemoryAvailable uint64    `json:"memoryAvailable"`
	SwapTotal       uint64    `json:"swapTotal"`
	SwapFree        uint64    `json:"swapFree"`
	DiskTotal       uint64    `json:"diskTotal"`
	DiskFree        uint64    `json:"diskFree"`
	Load            []float64 `json:"load"`
	Uptime          uint64    `json:"uptime"`
	Hostname        string    `json:"hostname"`
	Kernel          string    `json:"kernel"`
	CollectedAt     time.Time `json:"collectedAt"`
}

func Collect(dataPath string) Metrics {
	m := Metrics{CPUCount: runtime.NumCPU(), Load: []float64{0, 0, 0}, CollectedAt: time.Now().UTC()}
	m.Hostname, _ = os.Hostname()
	m.Kernel = runtime.GOOS + "/" + runtime.GOARCH
	m.CPUPercent = cpuPercent()
	values := readMemInfo()
	m.MemoryTotal = values["MemTotal"]
	m.MemoryAvailable = values["MemAvailable"]
	m.SwapTotal = values["SwapTotal"]
	m.SwapFree = values["SwapFree"]
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
	if syscall.Statfs(dataPath, &stat) == nil {
		m.DiskTotal = stat.Blocks * uint64(stat.Bsize)
		m.DiskFree = stat.Bavail * uint64(stat.Bsize)
	}
	return m
}

func readMemInfo() map[string]uint64 {
	values := map[string]uint64{}
	file, err := os.Open("/proc/meminfo")
	if err != nil {
		return values
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 2 {
			continue
		}
		if value, err := strconv.ParseUint(fields[1], 10, 64); err == nil {
			values[strings.TrimSuffix(fields[0], ":")] = value * 1024
		}
	}
	return values
}

func cpuPercent() float64 {
	total1, idle1, ok := readCPUStat()
	if !ok {
		return 0
	}
	time.Sleep(100 * time.Millisecond)
	total2, idle2, ok := readCPUStat()
	if !ok || total2 <= total1 {
		return 0
	}
	busy := (total2 - total1) - (idle2 - idle1)
	return float64(busy) * 100 / float64(total2-total1)
}

func readCPUStat() (uint64, uint64, bool) {
	body, err := os.ReadFile("/proc/stat")
	if err != nil {
		return 0, 0, false
	}
	line := strings.SplitN(string(body), "\n", 2)[0]
	fields := strings.Fields(line)
	if len(fields) < 5 || fields[0] != "cpu" {
		return 0, 0, false
	}
	var total uint64
	for _, field := range fields[1:] {
		value, err := strconv.ParseUint(field, 10, 64)
		if err != nil {
			return 0, 0, false
		}
		total += value
	}
	idle, err := strconv.ParseUint(fields[4], 10, 64)
	if err != nil {
		return 0, 0, false
	}
	if len(fields) > 5 {
		if wait, err := strconv.ParseUint(fields[5], 10, 64); err == nil {
			idle += wait
		}
	}
	return total, idle, true
}

func NowUnix() int64 { return time.Now().Unix() }
