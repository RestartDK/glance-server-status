// TODO:
// Try to make raw json body
// Make functions of the relevant parts to practice making functions and error handling
// For the functions I will only make it for the nested entries, not one for the host info cause it is not nested
// Try to type the json body using type system in go
// Properly handle errors, should not crash only if it can't understand basic host info
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

func getCPUInfo() map[string]any {
	info := map[string]any{}

	// Get load info
	loads, err := getLoadAverage()
	if err != nil {
		info["load_is_available"] = false
		info["load1_percent"] = 0
		info["load15_percent"] = 0
	} else {
		info["load_is_available"] = true
		info["load1_percent"] = loads[0]
		info["load15_percent"] = loads[1]
	}

	// Getting cpu temp
	temperature, err := getCPUTemperature()
	if err != nil {
		info["temperature_is_available"] = false
		info["temperature_c"] = 0
		fmt.Printf("CPU temperature error %v\n", err)
	} else {
		info["temperature_is_available"] = true
		info["temperature_c"] = temperature
	}

	return info
}

func getMemoryInfo() map[string]any {
	info := map[string]any{}

	// Read /proc/meminfo for memory statistics
	content, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		info["memory_is_available"] = false
		info["total_mb"] = 0
		info["used_mb"] = 0
		info["used_percent"] = 0
		info["swap_is_available"] = false
		info["swap_total_mb"] = 0
		info["swap_used_mb"] = 0
		info["swap_used_percent"] = 0
		return info
	}

	// Parse memory info
	memInfo := parseMemInfo(string(content))

	// Calculate memory usage
	totalMB := memInfo["MemTotal"] / 1024
	availableMB := memInfo["MemAvailable"] / 1024
	usedMB := totalMB - availableMB
	usedPercent := int((float64(usedMB) / float64(totalMB)) * 100)

	// Calculate swap usage
	swapTotalMB := memInfo["SwapTotal"] / 1024
	swapFreeMB := memInfo["SwapFree"] / 1024
	swapUsedMB := swapTotalMB - swapFreeMB
	swapUsedPercent := 0
	if swapTotalMB > 0 {
		swapUsedPercent = int((float64(swapUsedMB) / float64(swapTotalMB)) * 100)
	}

	info["memory_is_available"] = true
	info["total_mb"] = totalMB
	info["used_mb"] = usedMB
	info["used_percent"] = usedPercent
	info["swap_is_available"] = true
	info["swap_total_mb"] = swapTotalMB
	info["swap_used_mb"] = swapUsedMB
	info["swap_used_percent"] = swapUsedPercent

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

func getMountpoints() []map[string]any {
	var mountpoints []map[string]any

	// Read /proc/mounts to get mount information
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

			// Get disk usage for this mountpoint
			usage, err := getDiskUsage(mountpoint)
			if err != nil {
				continue
			}

			mountpointInfo := map[string]any{
				"path":         mountpoint,
				"name":         mountpoint,
				"total_mb":     usage["total_mb"],
				"used_mb":      usage["used_mb"],
				"used_percent": usage["used_percent"],
			}

			mountpoints = append(mountpoints, mountpointInfo)
		}
	}

	return mountpoints
}

func getDiskUsage(path string) (map[string]int64, error) {
	// Use statvfs system call equivalent
	cmd := exec.Command("df", "-B1", path)
	output, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	lines := strings.Split(string(output), "\n")
	if len(lines) < 2 {
		return nil, fmt.Errorf("invalid df output")
	}

	// Parse the second line (first line is header)
	parts := strings.Fields(lines[1])
	if len(parts) < 4 {
		return nil, fmt.Errorf("invalid df output format")
	}

	totalBytes, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return nil, err
	}

	usedBytes, err := strconv.ParseInt(parts[2], 10, 64)
	if err != nil {
		return nil, err
	}

	totalMB := totalBytes / (1024 * 1024)
	usedMB := usedBytes / (1024 * 1024)
	usedPercent := int64((float64(usedBytes) / float64(totalBytes)) * 100)

	return map[string]int64{
		"total_mb":     totalMB,
		"used_mb":      usedMB,
		"used_percent": usedPercent,
	}, nil
}

func handler(w http.ResponseWriter, r *http.Request) {
	response := make(map[string]any)

	// Get uptime
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

	// Get hostname
	hostname, err := os.Hostname()
	if err != nil {
		response["hostname"] = nil
		fmt.Println("Could not resolve hostname %w", err)
	}

	// Get platform
	const platform string = runtime.GOOS

	response["host_info_is_available"] = true
	response["boot_time"] = time.Now().Unix() - int64(uptime)
	response["hostname"] = hostname
	response["platform"] = platform
	response["cpu"] = getCPUInfo()
	response["memory"] = getMemoryInfo()
	response["mountpoints"] = getMountpoints()

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
}

func main() {
	http.HandleFunc("/", handler)
	http.ListenAndServe(":8080", nil)
}
