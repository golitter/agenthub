package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestServiceAuthFailsClosedAndAcceptsBearer(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, test := range []struct {
		name     string
		expected string
		header   string
		status   int
	}{
		{name: "missing server token", status: http.StatusUnauthorized},
		{name: "missing client token", expected: "secret", status: http.StatusUnauthorized},
		{name: "wrong token", expected: "secret", header: "Bearer wrong", status: http.StatusUnauthorized},
		{name: "valid token", expected: "secret", header: "Bearer secret", status: http.StatusNoContent},
	} {
		t.Run(test.name, func(t *testing.T) {
			router := gin.New()
			router.Use(ServiceAuth(test.expected))
			router.POST("/internal", func(c *gin.Context) { c.Status(http.StatusNoContent) })
			req := httptest.NewRequest(http.MethodPost, "/internal", nil)
			if test.header != "" {
				req.Header.Set("Authorization", test.header)
			}
			resp := httptest.NewRecorder()
			router.ServeHTTP(resp, req)
			if resp.Code != test.status {
				t.Fatalf("status = %d, want %d", resp.Code, test.status)
			}
		})
	}
}
