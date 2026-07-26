package middleware

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

func JSONBodyLimit(limit int64, skipPrefixes ...string) gin.HandlerFunc {
	return func(c *gin.Context) {
		for _, prefix := range skipPrefixes {
			if pathHasPrefix(c.Request.URL.Path, prefix) {
				c.Next()
				return
			}
		}

		if c.Request.Body != nil && isJSONContentType(c.GetHeader("Content-Type")) {
			if c.Request.ContentLength > limit {
				c.AbortWithStatusJSON(http.StatusRequestEntityTooLarge, gin.H{
					"code": http.StatusRequestEntityTooLarge,
					"msg":  "json body exceeds size limit",
				})
				return
			}
			c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, limit)
		}

		c.Next()
	}
}

func pathHasPrefix(pathValue, prefix string) bool {
	prefix = strings.TrimRight(prefix, "/")
	return pathValue == prefix || strings.HasPrefix(pathValue, prefix+"/")
}

func isJSONContentType(contentType string) bool {
	mediaType := strings.ToLower(strings.TrimSpace(strings.Split(contentType, ";")[0]))
	return mediaType == "application/json" || strings.HasSuffix(mediaType, "+json")
}
