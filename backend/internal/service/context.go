package service

import "context"

type skillContextKey string

const (
	skillOwnerContextKey skillContextKey = "skill-owner-id"
	skillAdminContextKey skillContextKey = "skill-admin"
)

// WithSkillOwner binds the authenticated owner to a Skill upload/confirmation
// request.  An empty owner is intentional when API authentication is disabled.
func WithSkillOwner(ctx context.Context, ownerID string) context.Context {
	return context.WithValue(ctx, skillOwnerContextKey, ownerID)
}

func SkillOwnerFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	owner, _ := ctx.Value(skillOwnerContextKey).(string)
	return owner
}

func WithSkillAdmin(ctx context.Context, admin bool) context.Context {
	return context.WithValue(ctx, skillAdminContextKey, admin)
}

func SkillAdminFromContext(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	admin, _ := ctx.Value(skillAdminContextKey).(bool)
	return admin
}
