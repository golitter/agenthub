package gormdao

import (
	"errors"

	daoiface "agenthub/backend/internal/dao"
	"agenthub/backend/internal/model"
	"agenthub/backend/pkg/db"

	"gorm.io/gorm"
)

type ContactGroupDao struct{}

func NewContactGroupDao() *ContactGroupDao {
	return &ContactGroupDao{}
}

func (dao *ContactGroupDao) ListGroups() ([]model.ContactGroup, error) {
	var groups []model.ContactGroup
	if err := db.GetDB().Order("sort_order ASC, created_at ASC").Find(&groups).Error; err != nil {
		return nil, err
	}
	return groups, nil
}

func (dao *ContactGroupDao) ListItemsByGroupID(groupID string) ([]model.ContactGroupItem, error) {
	var items []model.ContactGroupItem
	if err := db.GetDB().Where("group_id = ?", groupID).Order("sort_order ASC").Find(&items).Error; err != nil {
		return nil, err
	}
	return items, nil
}

func (dao *ContactGroupDao) ListActiveTaskIDs() ([]string, error) {
	var taskIDs []string
	if err := db.GetDB().Model(&model.Task{}).Where("status = ?", "active").Pluck("task_id", &taskIDs).Error; err != nil {
		return nil, err
	}
	return taskIDs, nil
}

func (dao *ContactGroupDao) GroupExists(groupID string) (bool, error) {
	var count int64
	if err := db.GetDB().Model(&model.ContactGroup{}).Where("group_id = ?", groupID).Count(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
}

func (dao *ContactGroupDao) ActiveTaskExists(taskID string) (bool, error) {
	var count int64
	if err := db.GetDB().Model(&model.Task{}).Where("task_id = ? AND status = ?", taskID, "active").Count(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
}

func (dao *ContactGroupDao) ItemExists(groupID, taskID string) (bool, error) {
	var count int64
	if err := db.GetDB().Model(&model.ContactGroupItem{}).Where("group_id = ? AND task_id = ?", groupID, taskID).Count(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
}

func (dao *ContactGroupDao) CreateGroup(group model.ContactGroup) (*model.ContactGroup, error) {
	if err := db.GetDB().Create(&group).Error; err != nil {
		return nil, err
	}
	return &group, nil
}

func (dao *ContactGroupDao) UpdateGroupName(groupID, name string) (bool, error) {
	result := db.GetDB().Model(&model.ContactGroup{}).Where("group_id = ?", groupID).Update("name", name)
	if result.Error != nil {
		return false, result.Error
	}
	if result.RowsAffected > 0 {
		return true, nil
	}

	return dao.GroupExists(groupID)
}

func (dao *ContactGroupDao) DeleteGroupWithItems(groupID string) (bool, error) {
	found := true
	err := db.GetDB().Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("group_id = ?", groupID).Delete(&model.ContactGroupItem{}).Error; err != nil {
			return err
		}
		result := tx.Where("group_id = ?", groupID).Delete(&model.ContactGroup{})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			found = false
			return nil
		}
		return nil
	})
	if err != nil {
		return false, err
	}
	return found, nil
}

func (dao *ContactGroupDao) CreateItem(item model.ContactGroupItem) (*model.ContactGroupItem, error) {
	if err := db.GetDB().Create(&item).Error; err != nil {
		if isDuplicateKeyError(err) {
			return nil, errors.Join(daoiface.ErrDuplicate, err)
		}
		return nil, err
	}
	return &item, nil
}

func (dao *ContactGroupDao) DeleteItem(groupID, taskID string) (bool, error) {
	result := db.GetDB().Where("group_id = ? AND task_id = ?", groupID, taskID).Delete(&model.ContactGroupItem{})
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected > 0, nil
}
