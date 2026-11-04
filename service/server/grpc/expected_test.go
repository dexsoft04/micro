package grpc

import (
	"testing"

	"github.com/golang/protobuf/proto"
	pberr "github.com/micro/micro/v3/proto/errors"
	"github.com/micro/micro/v3/service/errors"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type expectedRejection struct{}

func (expectedRejection) Error() string       { return "session not found" }
func (expectedRejection) ExpectedError() bool { return true }

func TestExpectedStatusPreservesWireContract(t *testing.T) {
	s, err := status.New(codes.Unknown, "session not found").WithDetails(&pberr.Error{Id: "test", Code: 99, Detail: "detail"})
	if err != nil {
		t.Fatal(err)
	}
	converted := handlerStatusError(expectedRejection{}, s)
	if converted == nil || !errors.IsExpected(converted) {
		t.Fatal("lost failure or local classification")
	}
	if converted.Error() != s.Err().Error() || !proto.Equal(status.Convert(converted).Proto(), s.Proto()) {
		t.Fatalf("status changed: %v", converted)
	}
	ordinary := handlerStatusError(s.Err(), s)
	if errors.IsExpected(ordinary) || !proto.Equal(status.Convert(ordinary).Proto(), s.Proto()) {
		t.Fatalf("unclassified status changed: %v", ordinary)
	}
}
