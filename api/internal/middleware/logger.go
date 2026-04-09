package middleware

import (
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// Logger middleware with Zap
type Logger struct {
	logger *zap.Logger
}

// NewLogger creates a new Zap logger middleware
func NewLogger() (*Logger, error) {
	logger, err := zap.NewProduction()
	if err != nil {
		return nil, err
	}
	return &Logger{logger: logger}, nil
}

// NewDevelopmentLogger creates a development logger
func NewDevelopmentLogger() (*Logger, error) {
	logger, err := zap.NewDevelopment()
	if err != nil {
		return nil, err
	}
	return &Logger{logger: logger}, nil
}

// Middleware returns the Gin middleware function
func (l *Logger) Middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		path := c.Request.URL.Path
		query := c.Request.URL.RawQuery

		c.Next()

		latency := time.Since(start)
		status := c.Writer.Status()

		fields := []zap.Field{
			zap.Int("status", status),
			zap.Duration("latency", latency),
			zap.String("client_ip", c.ClientIP()),
			zap.String("method", c.Request.Method),
			zap.String("path", path),
			zap.String("query", query),
			zap.String("user_agent", c.Request.UserAgent()),
		}

		if len(c.Errors) > 0 {
			errs := make([]error, len(c.Errors))
			for i, e := range c.Errors {
				errs[i] = e
			}
			fields = append(fields, zap.Errors("errors", errs))
			l.logger.Error("Request failed", fields...)
		} else if status >= 500 {
			l.logger.Error("Server error", fields...)
		} else if status >= 400 {
			l.logger.Warn("Client error", fields...)
		} else {
			l.logger.Info("Request processed", fields...)
		}
	}
}

// GetLogger returns the underlying zap logger
func (l *Logger) GetLogger() *zap.Logger {
	return l.logger
}

// Sync flushes the logger
func (l *Logger) Sync() {
	l.logger.Sync()
}
