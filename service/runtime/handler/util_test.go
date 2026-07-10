package handler

import (
	"context"
	"testing"

	pb "github.com/micro/micro/v3/proto/runtime"
	"github.com/micro/micro/v3/service/runtime"
)

func TestToCreateOptionsDeserializesResources(t *testing.T) {
	opts := toCreateOptions(context.Background(), &pb.CreateOptions{
		Resources: &pb.Resources{
			CPU:              1000,
			Memory:           1048,
			EphemeralStorage: 2000,
		},
		ResourceRequests: &pb.Resources{
			CPU:              100,
			Memory:           200,
			EphemeralStorage: 2000,
		},
	})

	var createOptions runtime.CreateOptions
	for _, opt := range opts {
		opt(&createOptions)
	}

	if createOptions.Resources == nil || createOptions.Resources.CPU != 1000 || createOptions.Resources.Mem != 1048 || createOptions.Resources.Disk != 2000 {
		t.Fatalf("unexpected limits: %#v", createOptions.Resources)
	}
	if createOptions.ResourceRequests == nil || createOptions.ResourceRequests.CPU != 100 || createOptions.ResourceRequests.Mem != 200 || createOptions.ResourceRequests.Disk != 2000 {
		t.Fatalf("unexpected requests: %#v", createOptions.ResourceRequests)
	}
}
