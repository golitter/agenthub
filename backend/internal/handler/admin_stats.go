package handler

import (
	"time"

	"agenthub/backend/internal/model"
	"agenthub/backend/internal/vo"
	"agenthub/backend/pkg/db"

	"github.com/gin-gonic/gin"
)

type DailySession struct {
	Date  string `json:"date"`
	Count int    `json:"count"`
}

type MessageByAgent struct {
	AgentType string `json:"agentType"`
	Count     int    `json:"count"`
}

type StorageDay struct {
	Date string  `json:"date"`
	Size float64 `json:"size"`
}

type StatisticsResponse struct {
	DailySessions   []DailySession   `json:"dailySessions"`
	WeeklySessions  []DailySession   `json:"weeklySessions"`
	Labels          []string         `json:"labels"`
	TotalMessages   int              `json:"totalMessages"`
	MessagesByAgent []MessageByAgent `json:"messagesByAgent"`
	StorageDays     []StorageDay     `json:"storageDays"`
	StorageLabels   []string         `json:"storageLabels"`
}

func (h *AdminHandler) GetStatistics(c *gin.Context) {
	database := db.GetDB()
	now := time.Now()

	// Daily sessions - last 7 days
	var dailySessions []DailySession
	for i := 6; i >= 0; i-- {
		date := now.AddDate(0, 0, -i)
		dateStr := date.Format("2006-01-02")
		var count int64
		database.Model(&model.Session{}).
			Where("DATE(created_at) = ?", dateStr).
			Count(&count)
		dailySessions = append(dailySessions, DailySession{Date: dateStr, Count: int(count)})
	}

	// Weekly sessions - last 4 weeks
	var weeklySessions []DailySession
	for i := 3; i >= 0; i-- {
		weekStart := now.AddDate(0, 0, -7*(i+1))
		weekEnd := now.AddDate(0, 0, -7*i)
		var count int64
		database.Model(&model.Session{}).
			Where("created_at >= ? AND created_at < ?", weekStart, weekEnd).
			Count(&count)
		weeklySessions = append(weeklySessions, DailySession{
			Date:  weekStart.Format("01-02"),
			Count: int(count),
		})
	}

	// Labels
	labels := make([]string, 7)
	for i := 6; i >= 0; i-- {
		labels[6-i] = now.AddDate(0, 0, -i).Format("01-02")
	}

	// Total messages
	var totalMessages int64
	database.Model(&model.Message{}).Count(&totalMessages)

	// Messages by agent type
	var messagesByAgent []MessageByAgent
	database.Model(&model.Message{}).
		Select("agent_type as agent_type, COUNT(*) as count").
		Where("agent_type != ''").
		Group("agent_type").
		Scan(&messagesByAgent)

	// Storage trend (mock - would need actual file size tracking)
	var storageDays []StorageDay
	var storageLabels []string
	for i := 6; i >= 0; i-- {
		date := now.AddDate(0, 0, -i)
		storageLabels = append(storageLabels, date.Format("01-02"))
		storageDays = append(storageDays, StorageDay{
			Date: date.Format("01-02"),
			Size: float64(100 + (6-i)*10), // placeholder
		})
	}

	vo.OK(c, StatisticsResponse{
		DailySessions:   dailySessions,
		WeeklySessions:  weeklySessions,
		Labels:          labels,
		TotalMessages:   int(totalMessages),
		MessagesByAgent: messagesByAgent,
		StorageDays:     storageDays,
		StorageLabels:   storageLabels,
	})
}
