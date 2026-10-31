package api

import (
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestRequestObservabilitySetsCorrelationID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(requestObservability())
	r.GET("/health", func(c *gin.Context) { c.Status(204) })

	req := httptest.NewRequest("GET", "/health", nil)
	req.Header.Set(requestIDHeader, "test-request-123")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != 204 {
		t.Fatalf("status = %d, want 204", rec.Code)
	}
	if got := rec.Header().Get(requestIDHeader); got != "test-request-123" {
		t.Fatalf("request id = %q, want %q", got, "test-request-123")
	}
}

func TestRequestObservabilityRejectsNewlineRequestID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(requestObservability())
	r.GET("/health", func(c *gin.Context) { c.Status(204) })

	req := httptest.NewRequest("GET", "/health", nil)
	req.Header.Set(requestIDHeader, "bad\nvalue")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != 204 {
		t.Fatalf("status = %d, want 204", rec.Code)
	}
	if got := rec.Header().Get(requestIDHeader); got == "bad\nvalue" || got == "" {
		t.Fatalf("request id was not replaced: %q", got)
	}
}
