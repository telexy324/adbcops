package middleware

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
	"sync/atomic"
	"time"
	"unicode"

	"github.com/gin-gonic/gin"
)

var fallbackRequestIDCounter atomic.Uint64

const (
	RequestIDHeader = "X-Request-ID"
	RequestIDKey    = "request_id"
	maxRequestIDLen = 128
)

// RequestID ensures every request and response has a safe correlation ID.
func RequestID() gin.HandlerFunc {
	return func(c *gin.Context) {
		requestID := strings.TrimSpace(c.GetHeader(RequestIDHeader))
		if !validRequestID(requestID) {
			requestID = newRequestID()
		}

		c.Set(RequestIDKey, requestID)
		c.Header(RequestIDHeader, requestID)
		c.Next()
	}
}

// GetRequestID returns the correlation ID stored in the Gin context.
func GetRequestID(c *gin.Context) string {
	requestID, _ := c.Get(RequestIDKey)
	value, _ := requestID.(string)
	return value
}

func validRequestID(value string) bool {
	if value == "" || len(value) > maxRequestIDLen {
		return false
	}
	for _, r := range value {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			continue
		}
		switch r {
		case '-', '_', '.', ':':
			continue
		default:
			return false
		}
	}
	return true
}

func newRequestID() string {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err == nil {
		return "req-" + hex.EncodeToString(bytes)
	}

	// rand.Read failures are exceptionally rare. Combine time and an atomic
	// counter so the fallback remains unique within the process.
	return fmt.Sprintf("req-fallback-%d-%d", time.Now().UnixNano(), fallbackRequestIDCounter.Add(1))
}
