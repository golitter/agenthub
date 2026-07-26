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
