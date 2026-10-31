package api

import (
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

const requestIDHeader = "X-Request-ID"

// requestObservability adds a bounded correlation ID and emits one structured
// access event after every request. It deliberately logs the Gin route
// template instead of the raw URL so bearer-like path parameters are not
// copied into centralized logs.
func requestObservability() gin.HandlerFunc {
	// High-volume successful requests do not need to be persisted by default.
	// Set ELECTRU_ACCESS_LOG=1 when full request-level access events are
	// required for troubleshooting or external log aggregation.
	accessLogEnabled := os.Getenv("ELECTRU_ACCESS_LOG") == "1"
	return func(c *gin.Context) {
		requestID := strings.TrimSpace(c.GetHeader(requestIDHeader))
		if len(requestID) == 0 || len(requestID) > 128 || strings.ContainsAny(requestID, "\r\n") {
			requestID = uuid.NewString()
		}
		c.Set("request_id", requestID)
		c.Header(requestIDHeader, requestID)
		started := time.Now()

		c.Next()

		route := c.FullPath()
		if route == "" {
			route = "<unmatched>"
		}
		attrs := []any{
			"request_id", requestID,
			"method", c.Request.Method,
			"route", route,
			"status", c.Writer.Status(),
			"duration_ms", time.Since(started).Milliseconds(),
		}
		status := c.Writer.Status()
		if status >= http.StatusInternalServerError {
			slog.Error("http request completed", attrs...)
		} else if accessLogEnabled {
			slog.Info("http request completed", attrs...)
		}
	}
}

func requestID(c *gin.Context) string {
	if value, ok := c.Get("request_id"); ok {
		if id, ok := value.(string); ok {
			return id
		}
	}
	return ""
}
