package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"agenthub/backend/internal/conf"

	"github.com/gin-gonic/gin"
)

func TestAuthWithSkipsAllowsPublicPath(t *testing.T) {
	router := testAuthRouter(AuthWithSkips("secret", "/api/admin/auth"))

	req := httptest.NewRequest(http.MethodPost, "/api/admin/auth", nil)
	resp := httptest.NewRecorder()

	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.Code, http.StatusOK)
	}
}

func TestAdminAuthRejectsRegularUserToken(t *testing.T) {
	userToken, err := GenerateToken(&conf.JWTConfig{Secret: "secret", ExpireHours: 1}, 7, "user")
	if err != nil {
		t.Fatal(err)
	}
	router := testAuthRouter(AdminAuth("secret"))
	req := httptest.NewRequest(http.MethodPost, "/api/skills/upload", nil)
	req.Header.Set("Authorization", "Bearer "+userToken)
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)
	if resp.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", resp.Code, http.StatusUnauthorized)
	}
}

func TestAdminAuthAcceptsOnlyAdminToken(t *testing.T) {
	token, err := GenerateAdminToken("secret")
	if err != nil {
		t.Fatal(err)
	}
	router := testAuthRouter(AdminAuth("secret"))
	req := httptest.NewRequest(http.MethodPost, "/api/skills/upload", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.Code, http.StatusOK)
	}
}

func TestAuthWithSkipsAcceptsAccessTokenQuery(t *testing.T) {
	token, err := GenerateAdminToken("secret")
	if err != nil {
		t.Fatalf("GenerateAdminToken: %v", err)
	}
	router := testAuthRouter(AuthWithSkips("secret"))

	req := httptest.NewRequest(http.MethodGet, "/api/tasks/task-1/stream?access_token="+token, nil)
	resp := httptest.NewRecorder()

	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.Code, http.StatusOK)
	}
}

func TestAuthWithSkipsRejectsAccessTokenQueryForNonStreamRoutes(t *testing.T) {
	token, err := GenerateAdminToken("secret")
	if err != nil {
		t.Fatalf("GenerateAdminToken: %v", err)
	}
	router := testAuthRouter(AuthWithSkips("secret"))

	req := httptest.NewRequest(http.MethodGet, "/api/tasks?access_token="+token, nil)
	resp := httptest.NewRecorder()

	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", resp.Code, http.StatusUnauthorized)
	}
}

func TestAuthWithSkipsRejectsInvalidHeaderFormat(t *testing.T) {
	router := testAuthRouter(AuthWithSkips("secret"))

	req := httptest.NewRequest(http.MethodGet, "/api/tasks", nil)
	req.Header.Set("Authorization", "Token bad")
	resp := httptest.NewRecorder()

	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", resp.Code, http.StatusUnauthorized)
	}
}

func testAuthRouter(middleware gin.HandlerFunc) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(middleware)
	router.Any("/*path", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})
	return router
}
