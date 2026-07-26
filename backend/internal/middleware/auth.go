package middleware

import (
	"fmt"
	"strings"
	"time"

	"agenthub/backend/internal/conf"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
)

func GenerateToken(cfg *conf.JWTConfig, userID int64, userName string) (string, error) {
	claims := jwt.MapClaims{
		"user_id":   userID,
		"user_name": userName,
		"exp":       time.Now().Add(time.Duration(cfg.ExpireHours) * time.Hour).Unix(),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(cfg.Secret))
}

func Auth(secret string) gin.HandlerFunc {
	return AuthWithSkips(secret)
}

func AuthWithSkips(secret string, skipPaths ...string) gin.HandlerFunc {
	return func(c *gin.Context) {
		if authSkipPath(c.Request.URL.Path, skipPaths) {
			c.Next()
			return
		}

		authHeader := c.GetHeader("Authorization")
		tokenString, authHeaderValid := bearerToken(authHeader)
		if authHeader != "" && !authHeaderValid {
			c.AbortWithStatusJSON(401, gin.H{"code": 401, "msg": "invalid authorization format"})
			return
		}
		if tokenString == "" && allowsQueryAccessToken(c) {
			tokenString = strings.TrimSpace(c.Query("access_token"))
		}
		if tokenString == "" {
			c.AbortWithStatusJSON(401, gin.H{"code": 401, "msg": "missing authorization header"})
			return
		}

		token, err := jwt.Parse(tokenString, func(t *jwt.Token) (interface{}, error) {
			if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
				return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
			}
			return []byte(secret), nil
		})
		if err != nil || !token.Valid {
			c.AbortWithStatusJSON(401, gin.H{"code": 401, "msg": "invalid token"})
			return
		}

		claims, ok := token.Claims.(jwt.MapClaims)
		if !ok {
			c.AbortWithStatusJSON(401, gin.H{"code": 401, "msg": "invalid claims"})
			return
		}

		c.Set("userID", claims["user_id"])
		c.Set("userName", claims["user_name"])
		c.Next()
	}
}

func bearerToken(auth string) (string, bool) {
	if auth == "" {
		return "", true
	}
	parts := strings.SplitN(auth, " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return "", false
	}
	token := strings.TrimSpace(parts[1])
	return token, token != ""
}

func authSkipPath(pathValue string, skipPaths []string) bool {
	for _, skipPath := range skipPaths {
		if pathValue == skipPath {
			return true
		}
	}
	return false
}

func allowsQueryAccessToken(c *gin.Context) bool {
	return c.Request.Method == "GET" && strings.HasSuffix(c.Request.URL.Path, "/stream")
}
