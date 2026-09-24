package client

import (
	"context"
	"errors"
	"io"
	"reflect"
	"sync"
	"testing"
	"time"

	pb "github.com/micro/micro/v3/proto/broker"
	"github.com/micro/micro/v3/service/broker"
	serviceclient "github.com/micro/micro/v3/service/client"
)

type subscribeResult struct {
	stream pb.Broker_SubscribeService
	err    error
}

type subscribeCall struct {
	options serviceclient.CallOptions
}

type recordingBrokerService struct {
	mu      sync.Mutex
	results []subscribeResult
	calls   chan subscribeCall
}

func (s *recordingBrokerService) Publish(context.Context, *pb.PublishRequest, ...serviceclient.CallOption) (*pb.Empty, error) {
	return &pb.Empty{}, nil
}

func (s *recordingBrokerService) Subscribe(_ context.Context, _ *pb.SubscribeRequest, opts ...serviceclient.CallOption) (pb.Broker_SubscribeService, error) {
	var options serviceclient.CallOptions
	for _, opt := range opts {
		opt(&options)
	}
	s.calls <- subscribeCall{options: options}

	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.results) == 0 {
		return nil, errors.New("unexpected subscribe call")
	}
	result := s.results[0]
	s.results = s.results[1:]
	return result.stream, result.err
}

type testSubscribeStream struct {
	recvErr error
	closed  chan struct{}
	once    sync.Once
}

func newTestSubscribeStream(recvErr error) *testSubscribeStream {
	return &testSubscribeStream{recvErr: recvErr, closed: make(chan struct{})}
}

func (s *testSubscribeStream) Context() context.Context  { return context.Background() }
func (s *testSubscribeStream) SendMsg(interface{}) error { return nil }
func (s *testSubscribeStream) RecvMsg(interface{}) error { return nil }
func (s *testSubscribeStream) Close() error {
	s.once.Do(func() { close(s.closed) })
	return nil
}
func (s *testSubscribeStream) Recv() (*pb.Message, error) {
	if s.recvErr != nil {
		return nil, s.recvErr
	}
	<-s.closed
	return nil, io.EOF
}

func TestSubscribeReusesDiscoveryModeAfterStreamFailure(t *testing.T) {
	testSubscribeAddressMode(t, nil, nil)
}

func TestSubscribeReusesExplicitAddressesAfterStreamFailure(t *testing.T) {
	addresses := []string{"broker-a:8003", "broker-b:8003"}
	testSubscribeAddressMode(t, addresses, addresses)
}

func testSubscribeAddressMode(t *testing.T, configured, want []string) {
	t.Helper()
	first := newTestSubscribeStream(io.EOF)
	second := newTestSubscribeStream(nil)
	service := &recordingBrokerService{
		calls: make(chan subscribeCall, 2),
		results: []subscribeResult{
			{stream: first},
			{stream: second},
		},
	}

	var options []broker.Option
	if configured != nil {
		options = append(options, broker.Addrs(configured...))
	}
	b := NewBroker().(*serviceBroker)
	if err := b.Init(options...); err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	b.Client = service

	subscriber, err := b.Subscribe("events", func(*broker.Message) error { return nil })
	if err != nil {
		t.Fatalf("Subscribe() error = %v", err)
	}
	t.Cleanup(func() { _ = subscriber.Unsubscribe() })

	for callNumber := 1; callNumber <= 2; callNumber++ {
		select {
		case call := <-service.calls:
			var wantAddresses []string
			if callNumber == 2 {
				wantAddresses = want
			}
			if !reflect.DeepEqual(call.options.Address, wantAddresses) {
				t.Fatalf("subscribe call %d addresses = %v, want %v", callNumber, call.options.Address, wantAddresses)
			}
			if !call.options.AuthToken {
				t.Fatalf("subscribe call %d did not request the service auth token", callNumber)
			}
			if call.options.RequestTimeout != time.Hour {
				t.Fatalf("subscribe call %d timeout = %s, want %s", callNumber, call.options.RequestTimeout, time.Hour)
			}
		case <-time.After(time.Second):
			t.Fatalf("timed out waiting for subscribe call %d", callNumber)
		}
	}
}
