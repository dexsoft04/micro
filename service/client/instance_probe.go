package client

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"

	"github.com/micro/micro/v3/service/registry"
)

// These headers are owned by the framework, not application handlers.
const (
	InstanceProbeVersionHeader = "micro-instance-probe"
	InstanceProbeDomainHeader  = "micro-instance-domain"
	InstanceProbeUIDHeader     = "micro-instance-uid"
	InstanceProofHeader        = "micro-instance-proof"
)

var ErrInstanceProbeUnsupported = errors.New("verified instance probe is unsupported")

type instanceProbeKey struct{}

func probeFromOptions(opts CallOptions) *instanceProbe {
	if opts.Context == nil {
		return nil
	}
	p, _ := opts.Context.Value(instanceProbeKey{}).(*instanceProbe)
	return p
}

type instanceProbe struct {
	domain, uid string
	verified    atomic.Bool
}

// ProbeInstance executes a unary READ-ONLY request and accepts its result only
// when the transport verifies the responding instance. Older servers may execute
// the read without providing proof; their result is rejected. Never use this API
// for mutations. It does not change the behavior of ordinary calls.
func ProbeInstance(ctx context.Context, c Client, req Request, rsp interface{}, domain, uid string, opts ...CallOption) error {
	if ctx == nil || c == nil || req == nil || req.Stream() || !validInstanceIdentity(domain) || !validInstanceIdentity(uid) {
		return ErrInstanceProbeUnsupported
	}
	if _, ok := ctx.Deadline(); !ok {
		return fmt.Errorf("instance probe requires a deadline")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	probe := &instanceProbe{domain: domain, uid: uid}
	options := append([]CallOption(nil), opts...)
	options = append(options, func(o *CallOptions) {
		if o.Context == nil {
			o.Context = context.Background()
		}
		o.Context = context.WithValue(o.Context, instanceProbeKey{}, probe)
	})
	if err := c.Call(ctx, req, rsp, options...); err != nil {
		return err
	}
	if !probe.verified.Load() {
		return ErrInstanceProbeUnsupported
	}
	return nil
}

func validInstanceIdentity(value string) bool {
	return value != "" && strings.TrimSpace(value) == value && !strings.ContainsAny(value, "*?[]/\\\x00\r\n\t")
}

// InstanceProbeTarget is for transport implementations. An unsupported transport
// must reject the probe without treating a normal successful call as proof.
func InstanceProbeTarget(opts CallOptions) (domain, uid string, enabled bool) {
	p := probeFromOptions(opts)
	if p == nil {
		return "", "", false
	}
	return p.domain, p.uid, true
}

// ConfirmInstanceProbe records proof only after a successful transport response.
func ConfirmInstanceProbe(opts CallOptions, version, domain, uid string) error {
	p := probeFromOptions(opts)
	if p == nil {
		return nil
	}
	if version != "1" || domain != p.domain || uid != p.uid {
		return ErrInstanceProbeUnsupported
	}
	p.verified.Store(true)
	return nil
}

// LookupInstanceProbeRoutes uses the configured discovery capability directly.
// Ordinary router caches and remote routers without this capability cannot
// provide a bounded authoritative instance lookup. Address overrides are rejected.
func LookupInstanceProbeRoutes(ctx context.Context, req Request, opts CallOptions) ([]string, error) {
	domain, uid, enabled := InstanceProbeTarget(opts)
	if !enabled || opts.Router == nil || len(opts.Address) != 0 {
		return nil, ErrInstanceProbeUnsupported
	}
	snapshot, err := registry.ReadSnapshot(ctx, opts.Router.Options().Registry, registry.SnapshotQuery{
		Domain: domain, Service: req.Service(), MaxNodes: 256, MaxBytes: 4 << 20,
	})
	if err != nil {
		return nil, err
	}
	var addresses []string
	for _, service := range snapshot.Services {
		for _, node := range service.Nodes {
			if node.Id == uid && node.Address != "" {
				addresses = append(addresses, node.Address)
			}
		}
	}
	if len(addresses) != 1 {
		return nil, ErrInstanceProbeUnsupported
	}
	return addresses, nil
}

// NewInstanceProbeClient adapts generated unary clients for READ-ONLY probes.
// Ordinary calls must keep using their existing client. Streaming is unsupported.
func NewInstanceProbeClient(c Client, domain, uid string) Client {
	return instanceProbeClient{Client: c, domain: domain, uid: uid}
}

type instanceProbeClient struct {
	Client
	domain, uid string
}

func (c instanceProbeClient) Call(ctx context.Context, req Request, rsp interface{}, opts ...CallOption) error {
	return ProbeInstance(ctx, c.Client, req, rsp, c.domain, c.uid, opts...)
}
func (c instanceProbeClient) Stream(context.Context, Request, ...CallOption) (Stream, error) {
	return nil, ErrInstanceProbeUnsupported
}
