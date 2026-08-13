package middleware

import (
	"crypto/subtle"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

// ServiceAuth protects AgentEnd -> Backend callbacks with an independent
// bearer token. Missing server configuration deliberately fails closed.
func ServiceAuth(expected string) gin.HandlerFunc {
	expected = strings.TrimSpace(expected)
	return func(c *gin.Context) {
		parts := strings.SplitN(c.GetHeader("Authorization"), " ", 2)
		supplied := ""
		if len(parts) == 2 && strings.EqualFold(parts[0], "Bearer") {
			supplied = strings.TrimSpace(parts[1])
		}
		if expected == "" || supplied == "" || subtle.ConstantTimeCompare([]byte(supplied), []byte(expected)) != 1 {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"code": http.StatusUnauthorized,
				"msg":  "service authentication required",
			})
			return
		}
		c.Next()
	}
}
