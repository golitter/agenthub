package gormdao

import (
	"errors"
	"time"

	daoiface "agenthub/backend/internal/dao"
	"agenthub/backend/internal/model"
	"agenthub/backend/pkg/db"

	"gorm.io/gorm"
)

type SkillDao struct{}

func NewSkillDao() *SkillDao {
	return &SkillDao{}
}

func (dao *SkillDao) CountBuiltinByName(name string) (int64, error) {
	var count int64
	if err := db.GetDB().Model(&model.SkillHub{}).Where("name = ? AND builtin = ?", name, true).Count(&count).Error; err != nil {
		return 0, err
	}
	return count, nil
}

func (dao *SkillDao) CountByName(name string) (int64, error) {
	var count int64
	if err := db.GetDB().Model(&model.SkillHub{}).Where("name = ?", name).Count(&count).Error; err != nil {
		return 0, err
	}
	return count, nil
}

func (dao *SkillDao) CreateSkill(skill model.SkillHub) error {
	return db.GetDB().Create(&skill).Error
}

func (dao *SkillDao) ListSkills() ([]model.SkillHub, error) {
	var skills []model.SkillHub
	if err := db.GetDB().Order("builtin DESC, name ASC").Find(&skills).Error; err != nil {
		return nil, err
	}
	return skills, nil
}

func (dao *SkillDao) CountImportsBySkillName(name string) (int64, error) {
	var count int64
	if err := db.GetDB().Model(&model.AgentSkill{}).Where("skill_name = ?", name).Count(&count).Error; err != nil {
		return 0, err
	}
	return count, nil
}

func (dao *SkillDao) GetSkillByName(name string) (*model.SkillHub, error) {
	var skill model.SkillHub
	if err := db.GetDB().Where("name = ?", name).First(&skill).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &skill, nil
}

func (dao *SkillDao) GetSkillContent(name string) ([]byte, error) {
	var skill model.SkillHub
	if err := db.GetDB().Select("content").Where("name = ?", name).First(&skill).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return skill.Content, nil
}

func (dao *SkillDao) DeleteSkillCascade(name string) error {
	return db.GetDB().Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("skill_name = ?", name).Delete(&model.AgentSkill{}).Error; err != nil {
			return err
		}
		if err := tx.Where("name = ?", name).Delete(&model.SkillHub{}).Error; err != nil {
			return err
		}
		return nil
	})
}

func (dao *SkillDao) HasAgentSkill(sessionID, skillName string) (bool, error) {
	var count int64
	if err := db.GetDB().Model(&model.AgentSkill{}).Where("session_id = ? AND skill_name = ?", sessionID, skillName).Count(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
}

func (dao *SkillDao) CreateAgentSkill(skill model.AgentSkill) error {
	if err := db.GetDB().Create(&skill).Error; err != nil {
		if isDuplicateKeyError(err) {
			return errors.Join(daoiface.ErrDuplicate, err)
		}
		return err
	}
	return nil
}

func (dao *SkillDao) DeleteAgentSkill(sessionID, skillName string) error {
	return db.GetDB().Where("session_id = ? AND skill_name = ?", sessionID, skillName).Delete(&model.AgentSkill{}).Error
}

func (dao *SkillDao) UpsertSkillHub(name, description string, builtin bool) error {
	skill := model.SkillHub{
		Name:        name,
		Builtin:     builtin,
		Description: description,
	}
	return db.GetDB().Where("name = ?", name).Assign(map[string]interface{}{
		"description": description,
		"builtin":     builtin,
	}).FirstOrCreate(&skill).Error
}

func (dao *SkillDao) EnsureAgentSkill(sessionID, skillName, agentType string) error {
	var existing model.AgentSkill
	err := db.GetDB().Where("session_id = ? AND skill_name = ?", sessionID, skillName).First(&existing).Error
	if err == nil {
		return nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	if err := db.GetDB().Create(&model.AgentSkill{
		SessionID:  sessionID,
		SkillName:  skillName,
		AgentType:  agentType,
		ImportedAt: time.Now(),
	}).Error; err != nil {
		if isDuplicateKeyError(err) {
			return nil
		}
		return err
	}
	return nil
}

func (dao *SkillDao) ListBuiltinSkills() ([]model.SkillHub, error) {
	var skills []model.SkillHub
	if err := db.GetDB().Where("builtin = ?", true).Find(&skills).Error; err != nil {
		return nil, err
	}
	return skills, nil
}

func (dao *SkillDao) ListExternalSkillsBySession(sessionID string) ([]model.SkillHub, error) {
	var skills []model.SkillHub
	if err := db.GetDB().
		Table("skill_hubs").
		Select("skill_hubs.*").
		Joins("JOIN agent_skill ON agent_skill.skill_name = skill_hubs.name").
		Where("agent_skill.session_id = ? AND skill_hubs.builtin = ?", sessionID, false).
		Find(&skills).Error; err != nil {
		return nil, err
	}
	return skills, nil
}
