package gormdao

import (
	"errors"
	"testing"

	"gorm.io/gorm"
)

func TestIsDuplicateKeyError(t *testing.T) {
	if !isDuplicateKeyError(errors.New("Error 1062: Duplicate entry 'a-b' for key 'idx'")) {
		t.Fatal("MySQL duplicate entry was not recognized")
	}
	if !isDuplicateKeyError(gorm.ErrDuplicatedKey) {
		t.Fatal("gorm duplicate key sentinel was not recognized")
	}
	if isDuplicateKeyError(errors.New("connection refused")) {
		t.Fatal("non-duplicate error was recognized as duplicate")
	}
}
