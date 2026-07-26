package middleware

import (
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

// ipBucket 在一个时间窗口内跟踪单个 IP 的请求计数。
type ipBucket struct {
	count   int
	expires time.Time
}

// IPRateLimiter 是一个简单的按 IP 滑动窗口限流器。
type IPRateLimiter struct {
	mu      sync.Mutex
	buckets map[string]*ipBucket
	rate    int
	window  time.Duration
}

// NewIPRateLimiter 创建一个限流器：每个 IP 在 window 窗口内最多允许 rate 次请求。
func NewIPRateLimiter(rate int, window time.Duration) *IPRateLimiter {
	rl := &IPRateLimiter{
		buckets: make(map[string]*ipBucket),
		rate:    rate,
		window:  window,
	}
	go rl.cleanup()
	return rl
}

func (rl *IPRateLimiter) cleanup() {
	ticker := time.NewTicker(rl.window)
	defer ticker.Stop()
	for range ticker.C {
		rl.mu.Lock()
		now := time.Now()
		for ip, b := range rl.buckets {
			if now.After(b.expires) {
				delete(rl.buckets, ip)
			}
		}
		rl.mu.Unlock()
	}
}

// Middleware 返回一个执行该限流策略的 Gin 中间件。
func (rl *IPRateLimiter) Middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		ip := c.ClientIP()
		rl.mu.Lock()
		b, ok := rl.buckets[ip]
		now := time.Now()
		if !ok || now.After(b.expires) {
			b = &ipBucket{count: 0, expires: now.Add(rl.window)}
			rl.buckets[ip] = b
		}
		b.count++
		allowed := b.count <= rl.rate
		rl.mu.Unlock()

		if !allowed {
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
				"code": http.StatusTooManyRequests,
				"msg":  "too many requests",
			})
			return
		}
		c.Next()
	}
}
