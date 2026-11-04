package errors

import (
	"context"
	stderrors "errors"
	"fmt"
	"testing"
)

type classifiedError bool

func (classifiedError) Error() string         { return "session not found" }
func (e classifiedError) ExpectedError() bool { return bool(e) }

func TestIsExpectedRequiresExplicitClassification(t *testing.T) {
	for _, tt := range []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"classified", classifiedError(true), true},
		{"declined", classifiedError(false), false},
		{"same text", stderrors.New("session not found"), false},
		{"canceled", context.Canceled, false},
		{"deadline", context.DeadlineExceeded, false},
		{"wrapped", fmt.Errorf("failed: %w", classifiedError(true)), false},
		{"mixed errors", stderrors.Join(classifiedError(true), context.DeadlineExceeded), false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsExpected(tt.err); got != tt.want {
				t.Fatalf("IsExpected(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}
