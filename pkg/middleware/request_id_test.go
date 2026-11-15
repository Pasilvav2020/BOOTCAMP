package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

func TestRequestIDAddsResponseHeaderAndStructuredLog(t *testing.T) {
	gin.SetMode(gin.TestMode)
	core, logs := observer.New(zap.InfoLevel)
	router := gin.New()
	router.Use(RequestID(zap.New(core)))
	router.GET("/orders/:id", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/orders/42?token=must-not-be-logged", nil))

	requestID := response.Header().Get("X-Request-ID")
	if requestID == "" {
		t.Fatal("X-Request-ID header is empty")
	}
	if logs.Len() != 1 {
		t.Fatalf("logged %d entries, want 1", logs.Len())
	}
	entry := logs.All()[0]
	if entry.Message != "HTTP request completed" {
		t.Errorf("log message = %q, want structured request completion", entry.Message)
	}
	if entry.ContextMap()["request_id"] != requestID || entry.ContextMap()["route"] != "/orders/:id" {
		t.Errorf("log fields = %v, want matching request ID and route template", entry.ContextMap())
	}
	if entry.ContextMap()["token"] != nil {
		t.Error("query string was included in structured request log")
	}
}
