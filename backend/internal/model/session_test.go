package model

import (
	"reflect"
	"strings"
	"testing"
)

func TestSessionStatusDefaultIsIdle(t *testing.T) {
	field, ok := reflect.TypeOf(Session{}).FieldByName("Status")
	if !ok {
		t.Fatal("Session.Status field not found")
	}
	tag := field.Tag.Get("gorm")
	if !strings.Contains(tag, "default:idle") {
		t.Fatalf("Session.Status gorm tag = %q, want default:idle", tag)
	}
	if strings.Contains(tag, "default:running") {
		t.Fatalf("Session.Status gorm tag still uses running default: %q", tag)
	}
}
