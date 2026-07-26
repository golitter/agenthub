package impl

import (
	"errors"
	"strings"

	"agenthub/backend/internal/dao"
	"agenthub/backend/internal/model"
	"agenthub/backend/internal/service"

	"github.com/google/uuid"
)

type ContactGroupService struct {
	dao dao.ContactGroupDao
}

const maxContactGroupNameLen = 80
const maxContactGroupIDLen = 36

func NewContactGroupService(contactGroupDao dao.ContactGroupDao) *ContactGroupService {
	return &ContactGroupService{dao: contactGroupDao}
}

func (svc *ContactGroupService) ListGroups() (*service.ListGroupsResponse, error) {
	groups, err := svc.dao.ListGroups()
	if err != nil {
		return nil, err
	}

	groupedSet := make(map[string]bool)
	result := make([]service.GroupWithItems, 0, len(groups))
	for _, group := range groups {
		items, err := svc.dao.ListItemsByGroupID(group.GroupID)
		if err != nil {
			return nil, err
		}

		groupItems := make([]service.GroupItem, 0, len(items))
		for _, item := range items {
			groupItems = append(groupItems, service.GroupItem{
				TaskID:    item.TaskID,
				SortOrder: item.SortOrder,
			})
			groupedSet[item.TaskID] = true
		}

		result = append(result, service.GroupWithItems{
			GroupID:   group.GroupID,
			Name:      group.Name,
			SortOrder: group.SortOrder,
			Items:     groupItems,
		})
	}

	taskIDs, err := svc.dao.ListActiveTaskIDs()
	if err != nil {
		return nil, err
	}
	ungrouped := make([]string, 0)
	for _, taskID := range taskIDs {
		if !groupedSet[taskID] {
			ungrouped = append(ungrouped, taskID)
		}
	}

	return &service.ListGroupsResponse{
		Groups:           result,
		UngroupedTaskIDs: ungrouped,
	}, nil
}

func (svc *ContactGroupService) CreateGroup(name string) (*model.ContactGroup, error) {
	normalized, err := normalizeContactGroupName(name)
	if err != nil {
		return nil, err
	}
	return svc.dao.CreateGroup(model.ContactGroup{
		GroupID: uuid.New().String(),
		Name:    normalized,
	})
}

func (svc *ContactGroupService) UpdateGroup(groupID, name string) error {
	groupID, err := normalizeContactGroupID(groupID)
	if err != nil {
		return err
	}
	normalized, err := normalizeContactGroupName(name)
	if err != nil {
		return err
	}
	updated, err := svc.dao.UpdateGroupName(groupID, normalized)
	if err != nil {
		return err
	}
	if !updated {
		return service.ErrNotFound("group not found")
	}
	return nil
}

func normalizeContactGroupName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", service.ErrBadRequest("group name is required")
	}
	if len([]rune(name)) > maxContactGroupNameLen {
		return "", service.ErrBadRequest("group name is too long")
	}
	return name, nil
}

func (svc *ContactGroupService) DeleteGroup(groupID string) error {
	groupID, err := normalizeContactGroupID(groupID)
	if err != nil {
		return err
	}
	deleted, err := svc.dao.DeleteGroupWithItems(groupID)
	if err != nil {
		return err
	}
	if !deleted {
		return service.ErrNotFound("group not found")
	}
	return nil
}

func (svc *ContactGroupService) AddItem(groupID, taskID string) (*model.ContactGroupItem, error) {
	groupID, err := normalizeContactGroupID(groupID)
	if err != nil {
		return nil, err
	}
	taskID, err = normalizeTaskID(taskID)
	if err != nil {
		return nil, err
	}
	groupExists, err := svc.dao.GroupExists(groupID)
	if err != nil {
		return nil, err
	}
	if !groupExists {
		return nil, service.ErrNotFound("group not found")
	}

	taskExists, err := svc.dao.ActiveTaskExists(taskID)
	if err != nil {
		return nil, err
	}
	if !taskExists {
		return nil, service.ErrNotFound("task not found")
	}

	itemExists, err := svc.dao.ItemExists(groupID, taskID)
	if err != nil {
		return nil, err
	}
	if itemExists {
		return nil, service.ErrConflict("item already exists")
	}

	item, err := svc.dao.CreateItem(model.ContactGroupItem{
		GroupID: groupID,
		TaskID:  taskID,
	})
	if err != nil {
		if errors.Is(err, dao.ErrDuplicate) {
			return nil, service.ErrConflict("item already exists")
		}
		return nil, err
	}
	return item, nil
}

func (svc *ContactGroupService) RemoveItem(groupID, taskID string) error {
	groupID, err := normalizeContactGroupID(groupID)
	if err != nil {
		return err
	}
	taskID, err = normalizeTaskID(taskID)
	if err != nil {
		return err
	}
	deleted, err := svc.dao.DeleteItem(groupID, taskID)
	if err != nil {
		return err
	}
	if !deleted {
		return service.ErrNotFound("item not found")
	}
	return nil
}

func normalizeContactGroupID(groupID string) (string, error) {
	groupID = strings.TrimSpace(groupID)
	if groupID == "" {
		return "", service.ErrBadRequest("group_id is required")
	}
	if len([]rune(groupID)) > maxContactGroupIDLen {
		return "", service.ErrBadRequest("group_id is too long")
	}
	return groupID, nil
}
