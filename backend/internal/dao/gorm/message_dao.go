package gormdao

import (
	"errors"

	"agenthub/backend/internal/model"
	"agenthub/backend/pkg/db"

	"gorm.io/gorm"
)

type MessageDao struct{}

func NewMessageDao() *MessageDao {
	return &MessageDao{}
}

func (dao *MessageDao) ListByTask(taskID, sessionID, mode, primarySessionID string, limit int, beforeID *uint64) ([]model.Message, error) {
	query := db.GetDB().Where("task_id = ?", taskID)
	if mode == "group" {
		query = applyGroupMessageVisibility(query, primarySessionID)
	} else if sessionID != "" {
		query = query.Where("session_id = ?", sessionID)
	}
	if beforeID != nil {
		query = query.Where("id < ?", *beforeID)
	}

	if beforeID == nil && limit == 0 {
		query = query.Order("created_at ASC").Order("id ASC")
	} else {
		query = query.Order("id DESC")
		if limit > 0 {
			query = query.Limit(limit)
		}
	}

	var messages []model.Message
	if err := query.Find(&messages).Error; err != nil {
		return nil, err
	}
	return messages, nil
}

func (dao *MessageDao) CountBySessionID(sessionID string) (int64, error) {
	var count int64
	if err := db.GetDB().Model(&model.Message{}).Where("session_id = ?", sessionID).Count(&count).Error; err != nil {
		return 0, err
	}
	return count, nil
}

func (dao *MessageDao) FindByMessageID(messageID string) (*model.Message, error) {
	var message model.Message
	if err := db.GetDB().Where("message_id = ?", messageID).First(&message).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &message, nil
}

func (dao *MessageDao) CreateMessage(message model.Message) error {
	return db.GetDB().Create(&message).Error
}

func (dao *MessageDao) FindSessionIDByTaskMessage(taskID, messageID string) (string, error) {
	var message model.Message
	if err := db.GetDB().Select("session_id").Where("task_id = ? AND message_id = ?", taskID, messageID).First(&message).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return "", nil
		}
		return "", err
	}
	return message.SessionID, nil
}

func (dao *MessageDao) FindMessageContent(messageID string) (string, error) {
	var message model.Message
	if err := db.GetDB().Select("content").Where("message_id = ?", messageID).First(&message).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return "", nil
		}
		return "", err
	}
	return message.Content, nil
}

func (dao *MessageDao) UpdateMessageContentAndSeq(messageID, content, seq string) error {
	return db.GetDB().Model(&model.Message{}).
		Where("message_id = ?", messageID).
		Updates(map[string]interface{}{
			"content":  content,
			"last_seq": seq,
		}).Error
}

func (dao *MessageDao) UpdateMessageStatus(messageID, status string) error {
	return db.GetDB().Model(&model.Message{}).
		Where("message_id = ?", messageID).
		Updates(map[string]interface{}{"status": status}).Error
}

func (dao *MessageDao) FailStaleStreamingMessages() (int64, error) {
	result := db.GetDB().Model(&model.Message{}).
		Where("status = ?", "streaming").
		Update("status", "failed")
	if result.Error != nil {
		return 0, result.Error
	}
	return result.RowsAffected, nil
}

func (dao *MessageDao) FindLatestCompletedAgentMessage(taskID, sessionID string) (*model.Message, error) {
	var message model.Message
	err := db.GetDB().
		Where("task_id = ? AND session_id = ? AND role = ? AND status = ?", taskID, sessionID, "agent", "completed").
		Order("created_at DESC").
		Limit(1).
		First(&message).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &message, nil
}

func (dao *MessageDao) ListGroupChatWindowMessages(taskID, sessionID string, lastMessage *model.Message) ([]model.Message, error) {
	query := db.GetDB().
		Where("task_id = ? AND session_id != ?", taskID, sessionID).
		Where("status IN ?", []string{"completed", "streaming"})
	if lastMessage != nil {
		query = query.Where("created_at > ?", lastMessage.CreatedAt)
	}

	var messages []model.Message
	if err := query.Order("created_at ASC").Order("id ASC").Find(&messages).Error; err != nil {
		return nil, err
	}
	return messages, nil
}

func (dao *MessageDao) FindLatestPlanReviewMessage(taskID, sessionID string) (*model.Message, error) {
	var message model.Message
	err := db.GetDB().
		Where("task_id = ? AND session_id = ? AND role = ? AND content LIKE ?", taskID, sessionID, "agent", "%type: plan_review%").
		Order("id DESC").
		First(&message).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &message, nil
}

func (dao *MessageDao) UpdateContent(messageID, content string) error {
	return db.GetDB().Model(&model.Message{}).
		Where("message_id = ?", messageID).
		Update("content", content).Error
}

func applyGroupMessageVisibility(query *gorm.DB, primarySessionID string) *gorm.DB {
	if primarySessionID == "" {
		return query.Where("role = ? OR role = ?", "user", "agent")
	}
	return query.Where("role = ? OR session_id = ?", "user", primarySessionID)
}
