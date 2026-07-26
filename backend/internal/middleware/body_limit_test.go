package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestJSONBodyLimitRejectsOversizedJSON(t *testing.T) {
	router := testBodyLimitRouter(JSONBodyLimit(4))

	req := httptest.NewRequest(http.MethodPost, "/api/tasks", strings.NewReader(`{"hello":"world"}`))
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()

	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want %d", resp.Code, http.StatusRequestEntityTooLarge)
	}
}

func TestJSONBodyLimitRecognizesStructuredJSONType(t *testing.T) {
	router := testBodyLimitRouter(JSONBodyLimit(4))

	req := httptest.NewRequest(http.MethodPost, "/api/tasks", strings.NewReader(`{"hello":"world"}`))
	req.Header.Set("Content-Type", "application/vnd.agenthub+json")
	resp := httptest.NewRecorder()

	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want %d", resp.Code, http.StatusRequestEntityTooLarge)
	}
}

func TestJSONBodyLimitSkipsConfiguredPrefix(t *testing.T) {
	router := testBodyLimitRouter(JSONBodyLimit(4, "/api/workspace"))

	req := httptest.NewRequest(http.MethodPost, "/api/workspace/ws-1/commit", strings.NewReader(`{"hello":"world"}`))
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()

	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.Code, http.StatusOK)
	}
}

func TestJSONBodyLimitDoesNotSkipPrefixWithoutPathBoundary(t *testing.T) {
	router := testBodyLimitRouter(JSONBodyLimit(4, "/api/workspace"))

	req := httptest.NewRequest(http.MethodPost, "/api/workspaceevil", strings.NewReader(`{"hello":"world"}`))
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()

	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want %d", resp.Code, http.StatusRequestEntityTooLarge)
	}
}

func testBodyLimitRouter(middleware gin.HandlerFunc) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(middleware)
	router.POST("/*path", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})
	return router
}
