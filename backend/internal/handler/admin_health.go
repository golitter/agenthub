package handler

import (
	"fmt"
	"net/http"
	"time"

	"agenthub/backend/internal/vo"

	"github.com/gin-gonic/gin"
)

type ServiceInfo struct {
	Name      string `json:"name"`
	Status    string `json:"status"`
	Uptime    string `json:"uptime"`
	Version   string `json:"version"`
	Port      int    `json:"port"`
	LastCheck string `json:"lastCheck"`
}

var startTime = time.Now()

func (h *AdminHandler) GetServices(c *gin.Context) {
	now := time.Now().Format("2006-01-02 15:04:05")

	services := []ServiceInfo{
		checkHTTPService("Frontend", "http://localhost:5173", 5173, now),
		checkHTTPService("Backend", "http://localhost:8080/ping", 8080, now),
		checkHTTPService("AgentEnd", fmt.Sprintf("http://localhost:%d/health", h.cfg.AgentEnd.Port), h.cfg.AgentEnd.Port, now),
	}

	vo.OK(c, services)
}

func checkHTTPService(name, url string, port int, lastCheck string) ServiceInfo {
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get(url)
	uptime := time.Since(startTime).Round(time.Second).String()

	status := "Down"
	if err == nil {
		resp.Body.Close()
		if resp.StatusCode >= 200 && resp.StatusCode < 400 {
			status = "Running"
		}
	}

	return ServiceInfo{
		Name:      name,
		Status:    status,
		Uptime:    uptime,
		Version:   "1.0.0",
		Port:      port,
		LastCheck: lastCheck,
	}
}
