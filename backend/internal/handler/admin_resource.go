package handler

import (
	"context"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"syscall"

	"agenthub/backend/internal/vo"
	"agenthub/backend/pkg/redis"

	"github.com/gin-gonic/gin"
)

type ResourceInfo struct {
	Used  float64 `json:"used"`
	Total float64 `json:"total"`
	Unit  string  `json:"unit"`
}

type ResourcesResponse struct {
	Disk   ResourceInfo `json:"disk"`
	Memory ResourceInfo `json:"memory"`
	Redis  ResourceInfo `json:"redis"`
}

func (h *AdminHandler) GetResources(c *gin.Context) {
	vo.OK(c, ResourcesResponse{
		Disk:   getDiskUsage(),
		Memory: getMemoryUsage(),
		Redis:  getRedisUsage(),
	})
}

func getDiskUsage() ResourceInfo {
	var stat syscall.Statfs_t
	if err := syscall.Statfs("/", &stat); err != nil {
		return ResourceInfo{Used: 0, Total: 0, Unit: "GB"}
	}
	total := float64(stat.Blocks*uint64(stat.Bsize)) / 1e9
	used := float64((stat.Blocks-stat.Bfree)*uint64(stat.Bsize)) / 1e9
	return ResourceInfo{Used: used, Total: total, Unit: "GB"}
}

func getMemoryUsage() ResourceInfo {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	_ = m

	// macOS: use sysctl hw.memsize for total, vm_stat for used
	out, err := exec.Command("sysctl", "-n", "hw.memsize").Output()
	if err != nil {
		return ResourceInfo{Used: 0, Total: 0, Unit: "GB"}
	}
	totalBytes, _ := strconv.ParseFloat(strings.TrimSpace(string(out)), 64)
	totalGB := totalBytes / 1e9

	// Parse vm_stat for free page count
	vmOut, err := exec.Command("vm_stat").Output()
	if err != nil {
		return ResourceInfo{Used: 0, Total: totalGB, Unit: "GB"}
	}
	freePages := parseVMStatPages(string(vmOut), "Pages free")
	inactivePages := parseVMStatPages(string(vmOut), "Pages inactive")
	pageSize := 4096.0 // macOS default
	freeGB := float64(freePages+inactivePages) * pageSize / 1e9
	usedGB := totalGB - freeGB

	return ResourceInfo{Used: usedGB, Total: totalGB, Unit: "GB"}
}

func parseVMStatPages(vmStat, prefix string) int {
	for _, line := range strings.Split(vmStat, "\n") {
		if strings.HasPrefix(line, prefix) {
			parts := strings.SplitN(line, ":", 2)
			if len(parts) == 2 {
				val := strings.TrimSpace(parts[1])
				val = strings.ReplaceAll(val, ".", "")
				n, _ := strconv.Atoi(strings.TrimSpace(val))
				return n
			}
		}
	}
	return 0
}

func getRedisUsage() ResourceInfo {
	client := redis.GetClient()
	if client == nil {
		return ResourceInfo{Used: 0, Total: 0, Unit: "MB"}
	}

	info, err := client.Info(context.Background(), "memory").Result()
	if err != nil {
		return ResourceInfo{Used: 0, Total: 0, Unit: "MB"}
	}

	usedMB := parseRedisInfoFloat(info, "used_memory") / 1e6
	maxMemoryStr := parseRedisInfoString(info, "maxmemory")
	var totalMB float64
	if maxMemoryStr != "" && maxMemoryStr != "0" {
		if val, err := strconv.ParseFloat(maxMemoryStr, 64); err == nil {
			totalMB = val / 1e6
		}
	}
	if totalMB == 0 {
		totalMB = 512
	}

	return ResourceInfo{Used: usedMB, Total: totalMB, Unit: "MB"}
}

func parseRedisInfoFloat(info, key string) float64 {
	target := key + ":"
	for i := 0; i < len(info); i++ {
		if i+len(target) <= len(info) && info[i:i+len(target)] == target {
			j := i + len(target)
			for j < len(info) && info[j] != '\r' && info[j] != '\n' {
				j++
			}
			val, _ := strconv.ParseFloat(info[i+len(target):j], 64)
			return val
		}
	}
	return 0
}

func parseRedisInfoString(info, key string) string {
	target := key + ":"
	for i := 0; i < len(info); i++ {
		if i+len(target) <= len(info) && info[i:i+len(target)] == target {
			j := i + len(target)
			for j < len(info) && info[j] != '\r' && info[j] != '\n' {
				j++
			}
			return info[i+len(target) : j]
		}
	}
	return ""
}
