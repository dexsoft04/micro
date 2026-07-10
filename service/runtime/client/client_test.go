package client

import (
	"context"
	"testing"

	pb "github.com/micro/micro/v3/proto/runtime"
	microclient "github.com/micro/micro/v3/service/client"
	"github.com/micro/micro/v3/service/runtime"
)

type fakeRuntimeService struct {
	createRequest *pb.CreateRequest
}

func (f *fakeRuntimeService) Create(ctx context.Context, in *pb.CreateRequest, opts ...microclient.CallOption) (*pb.CreateResponse, error) {
	f.createRequest = in
	return &pb.CreateResponse{}, nil
}

func (f *fakeRuntimeService) Read(ctx context.Context, in *pb.ReadRequest, opts ...microclient.CallOption) (*pb.ReadResponse, error) {
	return &pb.ReadResponse{}, nil
}

func (f *fakeRuntimeService) Delete(ctx context.Context, in *pb.DeleteRequest, opts ...microclient.CallOption) (*pb.DeleteResponse, error) {
	return &pb.DeleteResponse{}, nil
}

func (f *fakeRuntimeService) Update(ctx context.Context, in *pb.UpdateRequest, opts ...microclient.CallOption) (*pb.UpdateResponse, error) {
	return &pb.UpdateResponse{}, nil
}

func (f *fakeRuntimeService) Logs(ctx context.Context, in *pb.LogsRequest, opts ...microclient.CallOption) (pb.Runtime_LogsService, error) {
	return nil, nil
}

func TestCreateSerializesResources(t *testing.T) {
	fake := &fakeRuntimeService{}
	r := &svc{runtime: fake}

	err := r.Create(&runtime.Service{Name: "svc", Version: "latest"},
		runtime.ResourceLimits(&runtime.Resources{CPU: 1000, Mem: 1048, Disk: 2000}),
		runtime.ResourceRequests(&runtime.Resources{CPU: 100, Mem: 200, Disk: 2000}),
	)
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}
	if fake.createRequest == nil || fake.createRequest.Options == nil {
		t.Fatal("Create did not send options")
	}
	limits := fake.createRequest.Options.Resources
	if limits == nil || limits.CPU != 1000 || limits.Memory != 1048 || limits.EphemeralStorage != 2000 {
		t.Fatalf("unexpected serialized limits: %#v", limits)
	}
	requests := fake.createRequest.Options.ResourceRequests
	if requests == nil || requests.CPU != 100 || requests.Memory != 200 || requests.EphemeralStorage != 2000 {
		t.Fatalf("unexpected serialized requests: %#v", requests)
	}
}
