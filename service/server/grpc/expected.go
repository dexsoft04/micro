package grpc

import (
	"github.com/micro/micro/v3/service/errors"
	"github.com/micro/micro/v3/service/logger"
	"google.golang.org/grpc/status"
)

// Retain the local logging classification across status conversion without
// changing the code, message or details that gRPC sends to the caller.
type expectedStatusError struct {
	error
	status *status.Status
}

func (e expectedStatusError) GRPCStatus() *status.Status { return e.status }
func (expectedStatusError) ExpectedError() bool          { return true }

func handlerStatusError(appErr error, s *status.Status) error {
	err := s.Err()
	if err != nil && errors.IsExpected(appErr) {
		return expectedStatusError{error: err, status: s}
	}
	return err
}

func logHandlerError(err error) {
	if errors.IsExpected(err) {
		logger.Debugf("grpc handler rejected request: %s", err)
	} else if err != nil && logger.V(logger.InfoLevel, logger.DefaultLogger) {
		logger.Errorf("grpc handler got error: %s", err)
	}
}
