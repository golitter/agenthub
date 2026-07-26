package gormdao

import (
	"errors"
	"fmt"
	"strings"

	"agenthub/backend/internal/generated"
	"agenthub/backend/internal/model"
	"agenthub/backend/pkg/db"

	"gorm.io/gorm"
)

type MessageDao struct{}

const (
	maxMessageIDLen        = 36
	maxMessageTaskIDLen    = 36
	maxMessageSessionIDLen = 128
	maxMessageAgentTypeLen = 64
	maxMessageAgentNameLen = 128
	maxMessageGroupIDLen   = 64
)

func NewMessageDao() *MessageDao {
	return &MessageDao{}
}

func (dao *MessageDao) ListByTask(taskID, sessionID, mode, primarySessionID string, limit int, beforeID *uint64) ([]model.Message, error) {
	query := db.GetDB().Where("task_id = ?", taskID)
	if mode == "group" {
		query = applyGroupMessageVisibility(query, taskID, primarySessionID)
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
	normalized, err := normalizeCreateMessage(message)
	if err != nil {
		return err
	}
	return db.GetDB().Create(&normalized).Error
}

func normalizeCreateMessage(message model.Message) (model.Message, error) {
	message.MessageID = strings.TrimSpace(message.MessageID)
	message.TaskID = strings.TrimSpace(message.TaskID)
	message.SessionID = strings.TrimSpace(message.SessionID)
	message.Role = strings.TrimSpace(message.Role)
	message.Status = strings.TrimSpace(message.Status)
	message.AgentType = strings.TrimSpace(message.AgentType)
	message.AgentName = strings.TrimSpace(message.AgentName)
	message.GroupID = strings.TrimSpace(message.GroupID)

	if message.MessageID == "" {
		return message, fmt.Errorf("message_id is required")
	}
	if len([]rune(message.MessageID)) > maxMessageIDLen {
		return message, fmt.Errorf("message_id is too long")
	}
	if message.TaskID == "" {
		return message, fmt.Errorf("task_id is required")
	}
	if len([]rune(message.TaskID)) > maxMessageTaskIDLen {
		return message, fmt.Errorf("task_id is too long")
	}
	if message.SessionID == "" {
		return message, fmt.Errorf("session_id is required")
	}
	if len([]rune(message.SessionID)) > maxMessageSessionIDLen {
		return message, fmt.Errorf("session_id is too long")
	}
	if !isAllowedMessageRole(message.Role) {
		return message, fmt.Errorf("invalid message role: %s", message.Role)
	}
	if message.Status == "" {
		message.Status = string(generated.MessageStatusCompleted)
	}
	if !isAllowedMessageStatus(message.Status) {
		return message, fmt.Errorf("invalid message status: %s", message.Status)
	}
	if len([]rune(message.AgentType)) > maxMessageAgentTypeLen {
		return message, fmt.Errorf("agent_type is too long")
	}
	if len([]rune(message.AgentName)) > maxMessageAgentNameLen {
		return message, fmt.Errorf("agent_name is too long")
	}
	if len([]rune(message.GroupID)) > maxMessageGroupIDLen {
		return message, fmt.Errorf("group_id is too long")
	}
	return message, nil
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
	result := db.GetDB().Model(&model.Message{}).
		Where("message_id = ?", messageID).
		Updates(map[string]interface{}{
			"content":  content,
			"last_seq": seq,
		})
	if result.Error != nil {
		return result.Error
	}
	return ensureMessageUpdateFound(db.GetDB(), result.RowsAffected, messageID)
}

func (dao *MessageDao) UpdateMessageStatus(messageID, status string) error {
	if !isAllowedMessageStatus(status) {
		return fmt.Errorf("invalid message status: %s", status)
	}
	result := db.GetDB().Model(&model.Message{}).
		Where("message_id = ?", messageID).
		Updates(map[string]interface{}{"status": status})
	if result.Error != nil {
		return result.Error
	}
	return ensureMessageUpdateFound(db.GetDB(), result.RowsAffected, messageID)
}

func (dao *MessageDao) FailStaleStreamingMessages() (int64, error) {
	var rowsAffected int64
	err := db.GetDB().Transaction(func(tx *gorm.DB) error {
		var pairs []staleMessageSessionPair
		if err := staleStreamingSessionPairsQuery(tx).Scan(&pairs).Error; err != nil {
			return err
		}

		result := tx.Model(&model.Message{}).
			Where("status = ?", string(generated.MessageStatusStreaming)).
			Update("status", string(generated.MessageStatusFailed))
		if result.Error != nil {
			return result.Error
		}
		rowsAffected = result.RowsAffected

		for _, pair := range pairs {
			if err := tx.Model(&model.Session{}).
				Where("session_id = ? AND task_id = ? AND status IN ?", pair.SessionID, pair.TaskID, []string{
					string(generated.SessionStateRunning),
					string(generated.SessionStateAwaitingReview),
				}).
				Update("status", string(generated.SessionStateError)).Error; err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return rowsAffected, nil
}

type staleMessageSessionPair struct {
	SessionID string
	TaskID    string
}

func staleStreamingSessionPairsQuery(tx *gorm.DB) *gorm.DB {
	return tx.Model(&model.Message{}).
		Select("DISTINCT session_id, task_id").
		Where("status = ? AND session_id <> ? AND task_id <> ?", string(generated.MessageStatusStreaming), "", "")
}

func isAllowedMessageStatus(status string) bool {
	switch generated.MessageStatus(status) {
	case generated.MessageStatusStreaming,
		generated.MessageStatusCompleted,
		generated.MessageStatusFailed:
		return true
	default:
		return false
	}
}

func isAllowedMessageRole(role string) bool {
	switch generated.MessageRole(role) {
	case generated.MessageRoleUser,
		generated.MessageRoleAgent:
		return true
	default:
		return false
	}
}

func (dao *MessageDao) FindLatestCompletedAgentMessage(taskID, sessionID string) (*model.Message, error) {
	var message model.Message
	err := db.GetDB().
		Where("task_id = ? AND session_id = ? AND role = ? AND status = ?",
			taskID, sessionID, string(generated.MessageRoleAgent), string(generated.MessageStatusCompleted)).
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
		Where("status IN ?", []string{
			string(generated.MessageStatusCompleted),
			string(generated.MessageStatusStreaming),
		})
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
		Where("task_id = ? AND session_id = ? AND role = ? AND content LIKE ?",
			taskID, sessionID, string(generated.MessageRoleAgent), "%type: plan_review%").
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
	result := db.GetDB().Model(&model.Message{}).
		Where("message_id = ?", messageID).
		Update("content", content)
	if result.Error != nil {
		return result.Error
	}
	return ensureMessageUpdateFound(db.GetDB(), result.RowsAffected, messageID)
}

func ensureMessageUpdateFound(tx *gorm.DB, rowsAffected int64, messageID string) error {
	if rowsAffected > 0 {
		return nil
	}
	var count int64
	if err := tx.Model(&model.Message{}).Where("message_id = ?", messageID).Count(&count).Error; err != nil {
		return err
	}
	if count == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

func applyGroupMessageVisibility(query *gorm.DB, taskID, primarySessionID string) *gorm.DB {
	if primarySessionID == "" {
		return query.Where("role = ? OR role = ?", string(generated.MessageRoleUser), string(generated.MessageRoleAgent))
	}

	return query.Where(
		`role = ? OR session_id = ? OR (role = ? AND group_id <> ?) OR (
			role = ? AND session_id <> ? AND EXISTS (
				SELECT 1 FROM messages user_msg
				WHERE user_msg.task_id = messages.task_id
					AND user_msg.session_id = messages.session_id
					AND user_msg.role = ?
					AND user_msg.id < messages.id
					AND NOT EXISTS (
						SELECT 1 FROM messages agent_msg
						WHERE agent_msg.task_id = messages.task_id
							AND agent_msg.session_id = messages.session_id
							AND agent_msg.role = ?
							AND agent_msg.id > user_msg.id
							AND agent_msg.id < messages.id
					)
			)
		)`,
		string(generated.MessageRoleUser),
		primarySessionID,
		string(generated.MessageRoleAgent),
		"",
		string(generated.MessageRoleAgent),
		primarySessionID,
		string(generated.MessageRoleUser),
		string(generated.MessageRoleAgent),
	)
}
