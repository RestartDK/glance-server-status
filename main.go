package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// ServerStatus represents the complete server status response
type ServerStatus struct {
	HostInfo    bool         `json:"host_info_is_available"`
	BootTime    int64        `json:"boot_time"`
	Hostname    string       `json:"hostname"`
	Platform    string       `json:"platform"`
	CPU         CPUInfo      `json:"cpu"`
	Memory      MemoryInfo   `json:"memory"`
	Mountpoints []Mountpoint `json:"mountpoints"`
}

// CPUInfo represents CPU-related information
type CPUInfo struct {
	LoadAvailable bool  `json:"load_is_available"`
	Load1Percent  uint8 `json:"load1_percent"`
	Load15Percent uint8 `json:"load15_percent"`
	TempAvailable bool  `json:"temperature_is_available"`
	TemperatureC  int   `json:"temperature_c"`
}

// MemoryInfo represents memory and swap information
type MemoryInfo struct {
	MemoryAvailable bool  `json:"memory_is_available"`
	TotalMB         int64 `json:"total_mb"`
	UsedMB          int64 `json:"used_mb"`
	UsedPercent     uint8 `json:"used_percent"`
	SwapAvailable   bool  `json:"swap_is_available"`
	SwapTotalMB     int64 `json:"swap_total_mb"`
	SwapUsedMB      int64 `json:"swap_used_mb"`
	SwapUsedPercent uint8 `json:"swap_used_percent"`
}

// Mountpoint represents a filesystem mount point
type Mountpoint struct {
	Path        string `json:"path"`
	Name        string `json:"name"`
	TotalMB     int64  `json:"total_mb"`
	UsedMB      int64  `json:"used_mb"`
	UsedPercent uint8  `json:"used_percent"`
}

// DiskUsage represents disk usage information
type DiskUsage struct {
	TotalMB     int64 `json:"total_mb"`
	UsedMB      int64 `json:"used_mb"`
	UsedPercent uint8 `json:"used_percent"`
}

func getCPUTemperature() (int, error) {
	cmd := exec.Command("sensors")
	output, err := cmd.Output()
	if err != nil {
		return 0, fmt.Errorf("sensors command failed %w", err)
	}

	temperature, err := parseSensorsOutput(string(output))
	if err != nil {
		return 0, err
	}

	return temperature, nil
}

func parseSensorsOutput(output string) (int, error) {
	// First try to find "CPU: +XX.X°C" (from asusec)
	cpuRegex := regexp.MustCompile(`CPU:\s*\+(\d+)\.\d+°C`)
	matches := cpuRegex.FindStringSubmatch(output)
	if len(matches) >= 2 {
		temp, err := strconv.Atoi(matches[1])
		if err == nil {
			return temp, nil
		}
	}

	// Fallback to Tctl (from k10temp)
	tctlRegex := regexp.MustCompile(`Tctl:\s*\+(\d+)\.\d+°C`)
	matches = tctlRegex.FindStringSubmatch(output)
	if len(matches) >= 2 {
		temp, err := strconv.Atoi(matches[1])
		if err == nil {
			return temp, nil
		}
	}

	// Fallback to Tccd1 (CPU die temperature)
	tccdRegex := regexp.MustCompile(`Tccd1:\s*\+(\d+)\.\d+°C`)
	matches = tccdRegex.FindStringSubmatch(output)
	if len(matches) >= 2 {
		temp, err := strconv.Atoi(matches[1])
		if err == nil {
			return temp, nil
		}
	}

	return 0, fmt.Errorf("no CPU temperature found in sensors output")
}

func getLoadAverage() ([]int, error) {
	file, err := os.ReadFile("/proc/loadavg")
	if err != nil {
		return nil, fmt.Errorf("failed to read /proc/loadavg: %w", err)
	}

	loadsStr := strings.Fields(string(file))

	loads := make([]int, 3)

	for i := 0; i < 3; i++ {
		load, err := strconv.ParseFloat(loadsStr[i], 64)
		if err != nil {
			return nil, fmt.Errorf("failed to parse load %d: %w", i, err)
		}

		// Convert to percentage and cap at 100%
		loadPercent := int((load / float64(runtime.NumCPU())) * 100)
		if loadPercent > 100 {
			loadPercent = 100
		}

		loads[i] = loadPercent
	}

	return loads, nil
}

func getCPUInfo() CPUInfo {
	info := CPUInfo{}

	loads, err := getLoadAverage()
	if err != nil {
		info.LoadAvailable = false
		info.Load1Percent = 0
		info.Load15Percent = 0
	} else {
		info.LoadAvailable = true
		info.Load1Percent = uint8(loads[0])
		info.Load15Percent = uint8(loads[1])
	}

	temperature, err := getCPUTemperature()
	if err != nil {
		info.TempAvailable = false
		info.TemperatureC = 0
		fmt.Printf("CPU temperature error %v\n", err)
	} else {
		info.TempAvailable = true
		info.TemperatureC = temperature
	}

	return info
}

func getMemoryInfo() MemoryInfo {
	info := MemoryInfo{}

	content, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		info.MemoryAvailable = false
		info.TotalMB = 0
		info.UsedMB = 0
		info.UsedPercent = 0
		info.SwapAvailable = false
		info.SwapTotalMB = 0
		info.SwapUsedMB = 0
		info.SwapUsedPercent = 0
		return info
	}

	memInfo := parseMemInfo(string(content))

	// Calculate memory usage
	totalMB := memInfo["MemTotal"] / 1024
	availableMB := memInfo["MemAvailable"] / 1024
	usedMB := totalMB - availableMB
	usedPercent := uint8((float64(usedMB) / float64(totalMB)) * 100)

	// Calculate swap usage
	swapTotalMB := memInfo["SwapTotal"] / 1024
	swapFreeMB := memInfo["SwapFree"] / 1024
	swapUsedMB := swapTotalMB - swapFreeMB
	swapUsedPercent := uint8(0)
	if swapTotalMB > 0 {
		swapUsedPercent = uint8((float64(swapUsedMB) / float64(swapTotalMB)) * 100)
	}

	info.MemoryAvailable = true
	info.TotalMB = totalMB
	info.UsedMB = usedMB
	info.UsedPercent = usedPercent
	info.SwapAvailable = true
	info.SwapTotalMB = swapTotalMB
	info.SwapUsedMB = swapUsedMB
	info.SwapUsedPercent = swapUsedPercent

	return info
}

func parseMemInfo(content string) map[string]int64 {
	memInfo := make(map[string]int64)
	lines := strings.Split(content, "\n")

	for _, line := range lines {
		parts := strings.Fields(line)
		if len(parts) >= 2 {
			key := strings.TrimSuffix(parts[0], ":")
			value, err := strconv.ParseInt(parts[1], 10, 64)
			if err == nil {
				memInfo[key] = value
			}
		}
	}

	return memInfo
}

func getMountpoints() []Mountpoint {
	var mountpoints []Mountpoint

	content, err := os.ReadFile("/proc/mounts")
	if err != nil {
		return mountpoints
	}

	lines := strings.SplitSeq(string(content), "\n")

	for line := range lines {
		parts := strings.Fields(line)
		if len(parts) >= 3 {
			device := parts[0]
			mountpoint := parts[1]
			fstype := parts[2]

			// Skip special filesystems and snap mounts
			if strings.HasPrefix(mountpoint, "/snap") ||
				strings.HasPrefix(mountpoint, "/boot/efi") ||
				strings.HasPrefix(device, "/dev/loop") ||
				fstype == "tmpfs" ||
				fstype == "devtmpfs" ||
				fstype == "proc" ||
				fstype == "sysfs" {
				continue
			}

			usage, err := getDiskUsage(mountpoint)
			if err != nil {
				continue
			}

			mountpointInfo := Mountpoint{
				Path:        mountpoint,
				Name:        mountpoint,
				TotalMB:     usage.TotalMB,
				UsedMB:      usage.UsedMB,
				UsedPercent: usage.UsedPercent,
			}

			mountpoints = append(mountpoints, mountpointInfo)
		}
	}

	return mountpoints
}

func getDiskUsage(path string) (DiskUsage, error) {
	cmd := exec.Command("df", "-B1", path)
	output, err := cmd.Output()
	if err != nil {
		return DiskUsage{}, err
	}

	lines := strings.Split(string(output), "\n")
	if len(lines) < 2 {
		return DiskUsage{}, fmt.Errorf("invalid df output")
	}

	parts := strings.Fields(lines[1])
	if len(parts) < 4 {
		return DiskUsage{}, fmt.Errorf("invalid df output format")
	}

	totalBytes, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return DiskUsage{}, err
	}

	usedBytes, err := strconv.ParseInt(parts[2], 10, 64)
	if err != nil {
		return DiskUsage{}, err
	}

	totalMB := totalBytes / (1024 * 1024)
	usedMB := usedBytes / (1024 * 1024)
	usedPercent := uint8((float64(usedBytes) / float64(totalBytes)) * 100)

	return DiskUsage{
		TotalMB:     totalMB,
		UsedMB:      usedMB,
		UsedPercent: usedPercent,
	}, nil
}

func handler(w http.ResponseWriter, r *http.Request) {
	file, err := os.ReadFile("/proc/uptime")
	if err != nil {
		fmt.Println("No uptime found")
	}

	uptimeStr := string(file)
	times := strings.Fields(uptimeStr)

	uptime, err := strconv.ParseFloat(times[0], 64)
	if err != nil {
		fmt.Println("failed to parse uptime: %w", err)
	}

	hostname, err := os.Hostname()
	if err != nil {
		hostname = ""
		fmt.Println("Could not resolve hostname %w", err)
	}

	const platform string = runtime.GOOS

	response := ServerStatus{
		HostInfo:    true,
		BootTime:    time.Now().Unix() - int64(uptime),
		Hostname:    hostname,
		Platform:    platform,
		CPU:         getCPUInfo(),
		Memory:      getMemoryInfo(),
		Mountpoints: getMountpoints(),
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
}

func main() {
	http.HandleFunc("/", handler)
	http.ListenAndServe(":8080", nil)
}
