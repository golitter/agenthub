package middleware

import (
	"fmt"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
)

func VerifyAdminPassword(password, stored string) bool {
	if stored == "" {
		return false
	}
	// 若存储值看起来像 bcrypt 哈希，则使用 bcrypt 比较。
	if strings.HasPrefix(stored, "$2a$") || strings.HasPrefix(stored, "$2b$") {
		return bcrypt.CompareHashAndPassword([]byte(stored), []byte(password)) == nil
	}
	// 遗留的明文兜底比较——管理员应尽快在配置中改为 bcrypt 哈希。
	return password == stored
}

func GenerateAdminToken(jwtSecret string) (string, error) {
	claims := jwt.MapClaims{
		"admin": true,
		"exp":   time.Now().Add(1 * time.Hour).Unix(),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(jwtSecret))
}

func AdminAuth(jwtSecret string) gin.HandlerFunc {
	return func(c *gin.Context) {
		auth := c.GetHeader("Authorization")
		if auth == "" {
			c.AbortWithStatusJSON(401, gin.H{"code": 401, "msg": "missing authorization header"})
			return
		}

		parts := strings.SplitN(auth, " ", 2)
		if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
			c.AbortWithStatusJSON(401, gin.H{"code": 401, "msg": "invalid authorization format"})
			return
		}

		token, err := jwt.Parse(parts[1], func(t *jwt.Token) (interface{}, error) {
			if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
				return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
			}
			return []byte(jwtSecret), nil
		})
		if err != nil || !token.Valid {
			c.AbortWithStatusJSON(401, gin.H{"code": 401, "msg": "invalid or expired token"})
			return
		}

		claims, ok := token.Claims.(jwt.MapClaims)
		if !ok {
			c.AbortWithStatusJSON(401, gin.H{"code": 401, "msg": "invalid claims"})
			return
		}

		if isAdmin, _ := claims["admin"].(bool); !isAdmin {
			c.AbortWithStatusJSON(401, gin.H{"code": 401, "msg": "not an admin token"})
			return
		}

		c.Set("isAdmin", true)
		c.Next()
	}
}
