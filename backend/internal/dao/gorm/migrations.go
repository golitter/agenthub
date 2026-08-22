package gormdao

import (
	"context"
	"fmt"
	"sort"
	"time"

	"agenthub/backend/internal/model"
	"agenthub/backend/pkg/db"

	"gorm.io/gorm"
)

const migrationLockName = "agenthub_backend_schema_migrations"

type schemaMigrationRecord struct {
	Version   int64     `gorm:"primaryKey"`
	Name      string    `gorm:"size:255;not null"`
	AppliedAt time.Time `gorm:"not null"`
}

func (schemaMigrationRecord) TableName() string { return "schema_migrations" }

type migration struct {
	version int64
	name    string
	up      func(*gorm.DB) error
}

var backendMigrations = []migration{
	{
		version: 2026082201,
		name:    "baseline_backend_schema",
		up: func(gdb *gorm.DB) error {
			if err := cleanupDuplicateJoinRows(gdb); err != nil {
				return err
			}
			if err := gdb.AutoMigrate(
				&model.Session{}, &model.Task{}, &model.Message{}, &model.DiffSnapshot{},
				&model.SessionAgent{}, &model.AdminSetting{}, &model.Announcement{},
				&model.ContactGroup{}, &model.ContactGroupItem{}, &model.SkillHub{},
				&model.AgentSkill{}, &model.SkillUploadReceipt{}, &model.SkillOperationJob{},
				&model.SkillAuditEvent{}, &model.Artifact{},
			); err != nil {
				return err
			}
			return backfillSkillStorageMetadata(gdb)
		},
	},
	{
		version: 2026082202,
		name:    "create_task_cleanup_outbox",
		up: func(gdb *gorm.DB) error {
			if gdb.Migrator().HasTable(&model.TaskCleanupJob{}) {
				return nil
			}
			return gdb.Migrator().CreateTable(&model.TaskCleanupJob{})
		},
	},
}

// RunMigrations serializes schema changes across Backend replicas and records
// a version only after every step succeeds. Migration steps must be idempotent
// because MySQL may commit DDL even when a later statement fails.
func RunMigrations(ctx context.Context) error {
	gdb := db.GetDB()
	if gdb == nil {
		return fmt.Errorf("database is not initialized")
	}
	return gdb.WithContext(ctx).Connection(func(conn *gorm.DB) error {
		var locked int
		if err := conn.Raw("SELECT GET_LOCK(?, ?)", migrationLockName, 30).Scan(&locked).Error; err != nil {
			return fmt.Errorf("acquire migration lock: %w", err)
		}
		if locked != 1 {
			return fmt.Errorf("acquire migration lock: timed out")
		}
		defer func() {
			releaseCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = conn.WithContext(releaseCtx).Exec("SELECT RELEASE_LOCK(?)", migrationLockName).Error
		}()

		if err := conn.AutoMigrate(&schemaMigrationRecord{}); err != nil {
			return fmt.Errorf("bootstrap migration table: %w", err)
		}
		var records []schemaMigrationRecord
		if err := conn.Find(&records).Error; err != nil {
			return fmt.Errorf("read migration versions: %w", err)
		}
		applied := make(map[int64]bool, len(records))
		for _, record := range records {
			applied[record.Version] = true
		}
		return applyMigrationPlan(backendMigrations, applied, func(item migration) error {
			if err := item.up(conn); err != nil {
				return fmt.Errorf("migration %d (%s): %w", item.version, item.name, err)
			}
			return conn.Create(&schemaMigrationRecord{Version: item.version, Name: item.name, AppliedAt: time.Now().UTC()}).Error
		})
	})
}

func applyMigrationPlan(plan []migration, applied map[int64]bool, apply func(migration) error) error {
	ordered := append([]migration(nil), plan...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].version < ordered[j].version })
	var previous int64
	for _, item := range ordered {
		if item.version <= 0 || item.name == "" || item.up == nil {
			return fmt.Errorf("invalid migration registration for version %d", item.version)
		}
		if item.version == previous {
			return fmt.Errorf("duplicate migration version %d", item.version)
		}
		previous = item.version
		if applied[item.version] {
			continue
		}
		if err := apply(item); err != nil {
			return err
		}
		applied[item.version] = true
	}
	return nil
}

func cleanupDuplicateJoinRows(gdb *gorm.DB) error {
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
		return gdb.Exec(`
DELETE aks
FROM agent_skill aks
JOIN agent_skill kept
  ON aks.session_id = kept.session_id
 AND aks.skill_name = kept.skill_name
 AND aks.id > kept.id
`).Error
	}
	return nil
}

func backfillSkillStorageMetadata(gdb *gorm.DB) error {
	if !gdb.Migrator().HasTable(&model.SkillHub{}) {
		return nil
	}
	if err := gdb.Model(&model.SkillHub{}).
		Where("builtin = ? AND (storage_type = '' OR storage_type IS NULL) AND object_key IS NOT NULL AND object_key <> ''", false).
		Update("storage_type", model.SkillStorageMinIO).Error; err != nil {
		return err
	}
	if err := gdb.Model(&model.SkillHub{}).
		Where("builtin = ? AND (storage_type = '' OR storage_type IS NULL) AND (object_key = '' OR object_key IS NULL)", false).
		Update("storage_type", model.SkillStorageDB).Error; err != nil {
		return err
	}
	if err := gdb.Model(&model.SkillHub{}).Where("status = '' OR status IS NULL").Update("status", model.SkillStatusReady).Error; err != nil {
		return err
	}
	if err := gdb.Model(&model.SkillHub{}).Where("builtin = ?", true).Update("storage_type", "").Error; err != nil {
		return err
	}
	if gdb.Migrator().HasTable(&model.AgentSkill{}) {
		return gdb.Model(&model.AgentSkill{}).Where("status = '' OR status IS NULL").Update("status", model.AgentSkillStatusReady).Error
	}
	return nil
}
