package gormdao

import (
	"errors"
	"reflect"
	"testing"

	"gorm.io/gorm"
)

func TestApplyMigrationPlanOrdersAndSkipsAppliedVersions(t *testing.T) {
	plan := []migration{
		{version: 3, name: "third", up: func(*gorm.DB) error { return nil }},
		{version: 1, name: "first", up: func(*gorm.DB) error { return nil }},
		{version: 2, name: "second", up: func(*gorm.DB) error { return nil }},
	}
	var got []int64
	applied := map[int64]bool{1: true}
	if err := applyMigrationPlan(plan, applied, func(item migration) error {
		got = append(got, item.version)
		return nil
	}); err != nil {
		t.Fatalf("applyMigrationPlan: %v", err)
	}
	if want := []int64{2, 3}; !reflect.DeepEqual(got, want) {
		t.Fatalf("applied versions = %v, want %v", got, want)
	}
}

func TestApplyMigrationPlanDoesNotMarkFailedVersion(t *testing.T) {
	boom := errors.New("boom")
	applied := map[int64]bool{}
	err := applyMigrationPlan([]migration{
		{version: 1, name: "fails", up: func(*gorm.DB) error { return nil }},
		{version: 2, name: "never", up: func(*gorm.DB) error { return nil }},
	}, applied, func(item migration) error {
		if item.version == 1 {
			return boom
		}
		return nil
	})
	if !errors.Is(err, boom) {
		t.Fatalf("error = %v, want %v", err, boom)
	}
	if applied[1] || applied[2] {
		t.Fatalf("failed or later migration was marked applied: %#v", applied)
	}
}
