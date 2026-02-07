package tool

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

type SysInfoTool struct{}

func NewSysInfoTool() *SysInfoTool {
	return &SysInfoTool{}
}

func (t *SysInfoTool) Name() string { return "system_info" }
func (t *SysInfoTool) Description() string {
	return "Get detailed system information: CPU model & cores, total & used RAM, GPU, disk, OS version, hostname, and uptime."
}
func (t *SysInfoTool) Parameters() map[string]any {
	return map[string]any{
		"type":       "object",
		"properties": map[string]any{},
	}
}

func (t *SysInfoTool) Execute(ctx context.Context, args map[string]any) (string, error) {
	hostname, _ := os.Hostname()
	cwd, _ := os.Getwd()

	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)

	info := []string{
		"=== System Information ===",
		fmt.Sprintf("Hostname: %s", hostname),
		fmt.Sprintf("OS: %s/%s", runtime.GOOS, runtime.GOARCH),
	}

	if osVer := getOSVersion(ctx); osVer != "" {
		info = append(info, fmt.Sprintf("OS Version: %s", osVer))
	}

	info = append(info, "")
	info = append(info, "=== CPU ===")
	cpuName := getCPUName(ctx)
	if cpuName != "" {
		info = append(info, fmt.Sprintf("Model: %s", cpuName))
	}
	info = append(info, fmt.Sprintf("Logical Cores: %d", runtime.NumCPU()))
	if cpuExtra := getCPUExtra(ctx); cpuExtra != "" {
		info = append(info, cpuExtra)
	}

	info = append(info, "")
	info = append(info, "=== Memory (RAM) ===")
	ramInfo := getRAMInfo(ctx)
	if ramInfo != "" {
		info = append(info, ramInfo)
	} else {
		info = append(info, fmt.Sprintf("Go Process Alloc: %.1f MB", float64(mem.Alloc)/1024/1024))
		info = append(info, fmt.Sprintf("Go Process Sys: %.1f MB", float64(mem.Sys)/1024/1024))
	}

	info = append(info, "")
	info = append(info, "=== GPU ===")
	gpuInfo := getGPUInfo(ctx)
	if gpuInfo != "" {
		info = append(info, gpuInfo)
	} else {
		info = append(info, "Not detected")
	}

	info = append(info, "")
	info = append(info, "=== Disk ===")
	diskInfo := getDiskInfo(ctx)
	if diskInfo != "" {
		info = append(info, diskInfo)
	}

	info = append(info, "")
	info = append(info, "=== Runtime ===")
	info = append(info, fmt.Sprintf("Working Dir: %s", cwd))
	info = append(info, fmt.Sprintf("Go: %s", runtime.Version()))
	info = append(info, fmt.Sprintf("Goroutines: %d", runtime.NumGoroutine()))
	info = append(info, fmt.Sprintf("Time: %s", time.Now().Format(time.RFC3339)))
	info = append(info, fmt.Sprintf("Bot Uptime: %.0f seconds", time.Since(startTime).Seconds()))

	if uptime := getSystemUptime(ctx); uptime != "" {
		info = append(info, fmt.Sprintf("System Uptime: %s", uptime))
	}

	return strings.Join(info, "\n"), nil
}

var startTime = time.Now()

func runCmd(ctx context.Context, name string, args ...string) string {
	cmd := exec.CommandContext(ctx, name, args...)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = nil
	if err := cmd.Run(); err != nil {
		return ""
	}
	return strings.TrimSpace(out.String())
}

func getOSVersion(ctx context.Context) string {
	switch runtime.GOOS {
	case "darwin":
		ver := runCmd(ctx, "sw_vers", "-productVersion")
		name := runCmd(ctx, "sw_vers", "-productName")
		if name != "" && ver != "" {
			return fmt.Sprintf("%s %s", name, ver)
		}
		return ver
	case "linux":
		// Try /etc/os-release
		data, err := os.ReadFile("/etc/os-release")
		if err == nil {
			for _, line := range strings.Split(string(data), "\n") {
				if strings.HasPrefix(line, "PRETTY_NAME=") {
					return strings.Trim(strings.TrimPrefix(line, "PRETTY_NAME="), "\"")
				}
			}
		}
		return runCmd(ctx, "uname", "-r")
	}
	return ""
}

func getCPUName(ctx context.Context) string {
	switch runtime.GOOS {
	case "darwin":
		// Apple Silicon: chip name
		chip := runCmd(ctx, "sysctl", "-n", "machdep.cpu.brand_string")
		if chip != "" {
			return chip
		}
		return ""
	case "linux":
		data, err := os.ReadFile("/proc/cpuinfo")
		if err == nil {
			for _, line := range strings.Split(string(data), "\n") {
				if strings.HasPrefix(line, "model name") {
					parts := strings.SplitN(line, ":", 2)
					if len(parts) == 2 {
						return strings.TrimSpace(parts[1])
					}
				}
			}
		}
	}
	return ""
}

func getCPUExtra(ctx context.Context) string {
	switch runtime.GOOS {
	case "darwin":
		// Performance and efficiency cores (Apple Silicon)
		perf := runCmd(ctx, "sysctl", "-n", "hw.perflevel0.logicalcpu")
		eff := runCmd(ctx, "sysctl", "-n", "hw.perflevel1.logicalcpu")
		var parts []string
		if perf != "" {
			parts = append(parts, fmt.Sprintf("Performance Cores: %s", perf))
		}
		if eff != "" {
			parts = append(parts, fmt.Sprintf("Efficiency Cores: %s", eff))
		}
		if len(parts) > 0 {
			return strings.Join(parts, "\n")
		}
	}
	return ""
}

func getRAMInfo(ctx context.Context) string {
	switch runtime.GOOS {
	case "darwin":
		totalBytes := runCmd(ctx, "sysctl", "-n", "hw.memsize")
		var parts []string
		if totalBytes != "" {
			var total float64
			fmt.Sscanf(totalBytes, "%f", &total)
			totalGB := total / (1024 * 1024 * 1024)
			parts = append(parts, fmt.Sprintf("Total: %.0f GB", totalGB))
		}
		// Used memory from vm_stat
		vmstat := runCmd(ctx, "vm_stat")
		if vmstat != "" {
			var pageSize float64 = 16384 // Default for Apple Silicon
			var activePages, wiredPages, compressedPages float64
			for _, line := range strings.Split(vmstat, "\n") {
				line = strings.TrimSpace(line)
				if strings.Contains(line, "page size of") {
					fmt.Sscanf(line, "Mach Virtual Memory Statistics: (page size of %f bytes)", &pageSize)
				}
				if strings.HasPrefix(line, "Pages active:") {
					fmt.Sscanf(line, "Pages active: %f", &activePages)
				}
				if strings.HasPrefix(line, "Pages wired down:") {
					fmt.Sscanf(line, "Pages wired down: %f", &wiredPages)
				}
				if strings.HasPrefix(line, "Pages occupied by compressor:") {
					fmt.Sscanf(line, "Pages occupied by compressor: %f", &compressedPages)
				}
			}
			usedGB := (activePages + wiredPages + compressedPages) * pageSize / (1024 * 1024 * 1024)
			if usedGB > 0 {
				parts = append(parts, fmt.Sprintf("Used (approx): %.1f GB", usedGB))
			}
		}
		// Memory pressure
		pressure := runCmd(ctx, "sysctl", "-n", "kern.memorystatus_vm_pressure_level")
		if pressure != "" {
			level := "Normal"
			switch pressure {
			case "2":
				level = "Warning"
			case "4":
				level = "Critical"
			}
			parts = append(parts, fmt.Sprintf("Pressure: %s", level))
		}
		if len(parts) > 0 {
			return strings.Join(parts, "\n")
		}

	case "linux":
		data, err := os.ReadFile("/proc/meminfo")
		if err == nil {
			var total, available, free float64
			for _, line := range strings.Split(string(data), "\n") {
				if strings.HasPrefix(line, "MemTotal:") {
					fmt.Sscanf(line, "MemTotal: %f kB", &total)
				}
				if strings.HasPrefix(line, "MemAvailable:") {
					fmt.Sscanf(line, "MemAvailable: %f kB", &available)
				}
				if strings.HasPrefix(line, "MemFree:") {
					fmt.Sscanf(line, "MemFree: %f kB", &free)
				}
			}
			var parts []string
			if total > 0 {
				parts = append(parts, fmt.Sprintf("Total: %.1f GB", total/1024/1024))
			}
			if available > 0 {
				used := total - available
				parts = append(parts, fmt.Sprintf("Used: %.1f GB", used/1024/1024))
				parts = append(parts, fmt.Sprintf("Available: %.1f GB", available/1024/1024))
			} else if free > 0 {
				parts = append(parts, fmt.Sprintf("Free: %.1f GB", free/1024/1024))
			}
			if len(parts) > 0 {
				return strings.Join(parts, "\n")
			}
		}
	}
	return ""
}

func getGPUInfo(ctx context.Context) string {
	switch runtime.GOOS {
	case "darwin":
		// Use system_profiler for GPU info
		out := runCmd(ctx, "system_profiler", "SPDisplaysDataType", "-detailLevel", "basic")
		if out == "" {
			return ""
		}
		var parts []string
		for _, line := range strings.Split(out, "\n") {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "Graphics") || strings.HasPrefix(line, "Displays:") {
				continue
			}
			// Capture key GPU info lines
			for _, prefix := range []string{"Chipset Model:", "Type:", "Bus:", "VRAM", "Total Number of Cores:", "Vendor:", "Metal", "Resolution:"} {
				if strings.HasPrefix(line, prefix) {
					parts = append(parts, line)
					break
				}
			}
			// Also capture the GPU name (line ending with ":")
			if strings.HasSuffix(line, ":") && !strings.Contains(line, "Display") {
				parts = append(parts, fmt.Sprintf("Name: %s", strings.TrimSuffix(line, ":")))
			}
		}
		if len(parts) > 0 {
			return strings.Join(parts, "\n")
		}

	case "linux":
		// Try lspci
		out := runCmd(ctx, "lspci")
		if out != "" {
			var gpus []string
			for _, line := range strings.Split(out, "\n") {
				lower := strings.ToLower(line)
				if strings.Contains(lower, "vga") || strings.Contains(lower, "3d") || strings.Contains(lower, "display") {
					gpus = append(gpus, strings.TrimSpace(line))
				}
			}
			if len(gpus) > 0 {
				return strings.Join(gpus, "\n")
			}
		}
		// Try nvidia-smi
		nv := runCmd(ctx, "nvidia-smi", "--query-gpu=name,memory.total,memory.used,driver_version", "--format=csv,noheader")
		if nv != "" {
			return "NVIDIA: " + nv
		}
	}
	return ""
}

func getDiskInfo(ctx context.Context) string {
	out := runCmd(ctx, "df", "-h", "/")
	if out == "" {
		return "Not available"
	}
	lines := strings.Split(out, "\n")
	if len(lines) >= 2 {
		return lines[0] + "\n" + lines[1]
	}
	return out
}

func getSystemUptime(ctx context.Context) string {
	out := runCmd(ctx, "uptime")
	if out != "" {
		// Extract just the uptime part
		return strings.TrimSpace(out)
	}
	return ""
}
