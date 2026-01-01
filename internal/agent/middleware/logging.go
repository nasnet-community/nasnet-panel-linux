package middleware

import (
	"context"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
	"google.golang.org/grpc"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
)

// LoggingMiddleware provides gRPC interceptors for logging
type LoggingMiddleware struct{}

// NewLoggingMiddleware creates a new logging middleware
func NewLoggingMiddleware() *LoggingMiddleware {
	return &LoggingMiddleware{}
}

// UnaryServerInterceptor logs unary RPCs
func (m *LoggingMiddleware) UnaryServerInterceptor() grpc.UnaryServerInterceptor {
	return func(
		ctx context.Context,
		req interface{},
		info *grpc.UnaryServerInfo,
		handler grpc.UnaryHandler,
	) (interface{}, error) {
		start := time.Now()

		// Call the handler
		resp, err := handler(ctx, req)

		duration := time.Since(start)
		code := status.Code(err)

		// Extract peer info
		peerAddr := "unknown"
		if p, ok := peer.FromContext(ctx); ok {
			peerAddr = p.Addr.String()
		}

		fields := logrus.Fields{
			"method":      info.FullMethod,
			"peer":        peerAddr,
			"duration_ms": duration.Milliseconds(),
			"code":        code.String(),
		}

		if err != nil {
			fields["error"] = err.Error()
			logrus.WithFields(fields).Error("gRPC Request Failed")
		} else {
			// Choose level based on method to avoid noise
			// Read-only/High-frequency -> Debug
			// Modification -> Info
			if isHighFrequencyMethod(info.FullMethod) {
				logrus.WithFields(fields).Debug("gRPC Request Completed")
			} else {
				logrus.WithFields(fields).Info("gRPC Request Completed")
			}
		}

		return resp, err
	}
}

// StreamServerInterceptor logs streaming RPCs
func (m *LoggingMiddleware) StreamServerInterceptor() grpc.StreamServerInterceptor {
	return func(
		srv interface{},
		ss grpc.ServerStream,
		info *grpc.StreamServerInfo,
		handler grpc.StreamHandler,
	) error {
		start := time.Now()

		// Extract peer info
		peerAddr := "unknown"
		ctx := ss.Context()
		if p, ok := peer.FromContext(ctx); ok {
			peerAddr = p.Addr.String()
		}

		logger := logrus.WithFields(logrus.Fields{
			"method": info.FullMethod,
			"peer":   peerAddr,
			"type":   "stream",
		})

		if !isHighFrequencyMethod(info.FullMethod) {
			logger.Info("gRPC Stream Started")
		} else {
			logger.Debug("gRPC Stream Started")
		}

		err := handler(srv, ss)

		duration := time.Since(start)
		code := status.Code(err)

		endFields := logrus.Fields{
			"method":      info.FullMethod,
			"peer":        peerAddr,
			"duration_ms": duration.Milliseconds(),
			"code":        code.String(),
			"type":        "stream",
		}

		if err != nil && err != context.Canceled {
			endFields["error"] = err.Error()
			logrus.WithFields(endFields).Error("gRPC Stream Failed")
		} else {
			if isHighFrequencyMethod(info.FullMethod) {
				logrus.WithFields(endFields).Debug("gRPC Stream Ended")
			} else {
				logrus.WithFields(endFields).Info("gRPC Stream Ended")
			}
		}

		return err
	}
}

// isHighFrequencyMethod identifies methods that should be logged at debug level
func isHighFrequencyMethod(method string) bool {
	// Methods that are called very frequently
	if strings.Contains(method, "Heartbeat") {
		return true
	}
	if strings.Contains(method, "GetStatus") {
		return true
	}
	if strings.Contains(method, "GetSystemStats") {
		return true
	}
	if strings.Contains(method, "GetXrayStats") {
		return true
	}
	return false
}
