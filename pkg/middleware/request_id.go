package middleware

import (
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

const requestIDContextKey = "request_id"

func RequestID(log *zap.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		requestID := uuid.NewString()
		c.Set(requestIDContextKey, requestID)
		c.Header("X-Request-ID", requestID)

		startedAt := time.Now()
		c.Next()

		route := c.FullPath()
		if route == "" {
			route = "unmatched"
		}
		log.Info("HTTP request completed",
			zap.String("request_id", requestID),
			zap.String("method", c.Request.Method),
			zap.String("route", route),
			zap.Int("status", c.Writer.Status()),
			zap.Duration("duration", time.Since(startedAt)),
		)
	}
}

func RequestIDFromContext(c *gin.Context) string {
	requestID, _ := c.Get(requestIDContextKey)
	value, _ := requestID.(string)
	return value
}
