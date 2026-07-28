package mockdao

import "agenthub/backend/internal/model"

type DiffSnapshotDao struct {
	GetBySnapshotIDFunc        func(snapshotID string) (*model.DiffSnapshot, error)
	CancelPendingBySessionFunc func(sessionID, excludedSnapshotID string) error
	UpsertFunc                 func(snapshot model.DiffSnapshot) (*model.DiffSnapshot, error)
	UpsertPendingFunc          func(snapshotID, sessionID, diff string) error
}

func NewDiffSnapshotDao() *DiffSnapshotDao {
	return &DiffSnapshotDao{}
}

func (dao *DiffSnapshotDao) GetBySnapshotID(snapshotID string) (*model.DiffSnapshot, error) {
	if dao.GetBySnapshotIDFunc != nil {
		return dao.GetBySnapshotIDFunc(snapshotID)
	}

	return nil, nil
}

func (dao *DiffSnapshotDao) CancelPendingBySession(sessionID, excludedSnapshotID string) error {
	if dao.CancelPendingBySessionFunc != nil {
		return dao.CancelPendingBySessionFunc(sessionID, excludedSnapshotID)
	}

	return nil
}

func (dao *DiffSnapshotDao) Upsert(snapshot model.DiffSnapshot) (*model.DiffSnapshot, error) {
	if dao.UpsertFunc != nil {
		return dao.UpsertFunc(snapshot)
	}

	return &snapshot, nil
}

func (dao *DiffSnapshotDao) UpsertPending(snapshotID, sessionID, diff string) error {
	if dao.UpsertPendingFunc != nil {
		return dao.UpsertPendingFunc(snapshotID, sessionID, diff)
	}

	return nil
}
