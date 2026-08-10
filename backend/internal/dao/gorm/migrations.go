package gormdao

import (
	"agenthub/backend/internal/model"
	"agenthub/backend/pkg/db"
)

func CleanupDuplicateJoinRows() error {
	gdb := db.GetDB()
	if gdb == nil {
		return nil
	}

	if gdb.Migrator().HasTable(&model.ContactGroupItem{}) {
		if err := gdb.Exec(`
DELETE cgi
FROM contact_group_items cgi
JOIN contact_group_items kept
  ON cgi.group_id = kept.group_id
 AND cgi.task_id = kept.task_id
 AND cgi.id > kept.id
`).Error; err != nil {
			return err
		}
	}

	if gdb.Migrator().HasTable(&model.AgentSkill{}) {
		if err := gdb.Exec(`
DELETE aks
FROM agent_skill aks
JOIN agent_skill kept
  ON aks.session_id = kept.session_id
 AND aks.skill_name = kept.skill_name
 AND aks.id > kept.id
`).Error; err != nil {
			return err
		}
	}
	return nil
}

// BackfillSkillStorageMetadata makes the expand-only schema safe for existing rows.
// It is intentionally idempotent so startup can repeat it after AutoMigrate.
func BackfillSkillStorageMetadata() error {
	gdb := db.GetDB()
	if gdb == nil || !gdb.Migrator().HasTable(&model.SkillHub{}) {
		return nil
	}

	if err := gdb.Model(&model.SkillHub{}).
		Where("builtin = ? AND (storage_type = '' OR storage_type IS NULL)", false).
		Updates(map[string]interface{}{
			"storage_type": model.SkillStorageDB,
			"status":       model.SkillStatusReady,
		}).Error; err != nil {
		return err
	}
	if err := gdb.Model(&model.SkillHub{}).
		Where("status = '' OR status IS NULL").
		Update("status", model.SkillStatusReady).Error; err != nil {
		return err
	}
	if err := gdb.Model(&model.SkillHub{}).
		Where("builtin = ?", true).
		Update("storage_type", "").Error; err != nil {
		return err
	}
	if gdb.Migrator().HasTable(&model.AgentSkill{}) {
		return gdb.Model(&model.AgentSkill{}).
			Where("status = '' OR status IS NULL").
			Update("status", model.AgentSkillStatusReady).Error
	}
	return nil
}
