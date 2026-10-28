package handler

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/golang/protobuf/proto"
	ws "github.com/gorilla/websocket"
	gateway "github.com/micro/micro/v3/plugin/websocket"
	wire "github.com/micro/micro/v3/proto/transport"
	"github.com/micro/micro/v3/service/client"
	grpcclient "github.com/micro/micro/v3/service/client/grpc"
	"github.com/micro/micro/v3/service/network"
	"github.com/micro/micro/v3/service/network/transport"
	"github.com/micro/micro/v3/service/registry"
	registrymemory "github.com/micro/micro/v3/service/registry/memory"
	"github.com/micro/micro/v3/service/router"
	registryrouter "github.com/micro/micro/v3/service/router/registry"
	"github.com/micro/micro/v3/service/server"
	grpcserver "github.com/micro/micro/v3/service/server/grpc"
	pb "github.com/wolfplus2048/mcbeam-plugins/ws_session/v3/proto"
)

const routingHelperEnv = "MCBEAM_WS_ROUTING_TEST_HELPER"

type routingEndpoint struct {
	PID    int
	UID    string
	RPC    string
	Socket string
}

// Each helper owns a separate process-global sessionsBySID map. A single-process
// two-server test would incorrectly allow the wrong instance to find both SIDs.
func TestSessionRoutingHelperProcess(t *testing.T) {
	id := os.Getenv(routingHelperEnv)
	if id == "" {
		return
	}
	r := registrymemory.NewRegistry()
	s := grpcserver.NewServer(server.Name("websocket"), server.Id(id),
		server.Address("127.0.0.1:0"), server.Registry(r))
	if err := pb.RegisterSessionHandler(s, &Handler{}); err != nil {
		t.Fatal(err)
	}
	if err := s.Start(); err != nil {
		t.Fatal(err)
	}
	defer s.Stop()
	upgrader := ws.Upgrader{}
	h := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		conn, err := upgrader.Upgrade(w, req, nil)
		if err != nil {
			return
		}
		session := gateway.NewSession(conn, "micro")
		defer session.Close()
		// Only this fixture handshake is synthetic; all subsequent push frames are
		// produced by the real Session.Send RPC handler and websocket session.
		if err := session.Send(&transport.Message{Header: map[string]string{"Test-SID": session.SID()}}); err != nil {
			return
		}
		for {
			var heartbeat transport.Message
			if err := session.Recv(&heartbeat); err != nil {
				return
			}
		}
	}))
	defer h.Close()
	endpoint := routingEndpoint{PID: os.Getpid(), UID: "websocket-" + id,
		RPC: s.Options().Address, Socket: "ws" + strings.TrimPrefix(h.URL, "http")}
	encoded, err := json.Marshal(endpoint)
	if err != nil {
		t.Fatal(err)
	}
	fmt.Printf("ROUTING_READY %s\n", encoded)
	// The parent closes stdin to stop the helper; CommandContext also imposes a
	// hard deadline so a failed test cannot leave processes behind.
	_, _ = io.Copy(io.Discard, os.Stdin)
}

func startRoutingEndpoint(t *testing.T, id string) routingEndpoint {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestSessionRoutingHelperProcess$", "-test.timeout=25s")
	cmd.Env = append(os.Environ(), routingHelperEnv+"="+id)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		cancel()
		t.Fatal(err)
	}
	ready := make(chan routingEndpoint, 1)
	scanned := make(chan struct{})
	go func() {
		defer close(scanned)
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			if line := scanner.Text(); strings.HasPrefix(line, "ROUTING_READY ") {
				var endpoint routingEndpoint
				if json.Unmarshal([]byte(strings.TrimPrefix(line, "ROUTING_READY ")), &endpoint) == nil {
					ready <- endpoint
				}
			}
		}
	}()
	t.Cleanup(func() {
		_ = stdin.Close()
		<-scanned
		err := cmd.Wait()
		cancel()
		if err != nil {
			t.Errorf("routing helper %s failed: %v\n%s", id, err, stderr.String())
		}
	})
	select {
	case endpoint := <-ready:
		return endpoint
	case <-scanned:
		t.Fatal("routing helper exited before readiness")
	case <-time.After(10 * time.Second):
		cancel()
		t.Fatal("routing helper readiness timed out")
	}
	return routingEndpoint{}
}

type routingSocket struct {
	conn     *ws.Conn
	sid      string
	messages chan *wire.Message
	errors   chan error
	stop     chan struct{}
}

func openRoutingSocket(t *testing.T, endpoint routingEndpoint) *routingSocket {
	t.Helper()
	conn, _, err := ws.DefaultDialer.Dial(endpoint.Socket, nil)
	if err != nil {
		t.Fatal(err)
	}
	sock := &routingSocket{conn: conn, messages: make(chan *wire.Message, 8), errors: make(chan error, 1), stop: make(chan struct{})}
	t.Cleanup(func() { close(sock.stop); _ = conn.Close() })
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	_, data, err := conn.ReadMessage()
	if err != nil {
		t.Fatal(err)
	}
	var handshake wire.Message
	if err := proto.Unmarshal(data, &handshake); err != nil {
		t.Fatal(err)
	}
	sock.sid = handshake.Header["Test-SID"]
	if sock.sid == "" {
		t.Fatal("helper did not announce its session ID")
	}
	_ = conn.SetReadDeadline(time.Time{})
	go func() {
		for {
			_, data, err := conn.ReadMessage()
			message := new(wire.Message)
			if err == nil {
				err = proto.Unmarshal(data, message)
			}
			if err != nil {
				sock.errors <- err
				return
			}
			select {
			case sock.messages <- message:
			case <-sock.stop:
				return
			}
		}
	}()
	go func() {
		ticker := time.NewTicker(500 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-sock.stop:
				return
			case <-ticker.C:
				_ = conn.SetWriteDeadline(time.Now().Add(time.Second))
				// Empty protobuf Message is a valid heartbeat for Session.Recv.
				if conn.WriteMessage(ws.BinaryMessage, nil) != nil {
					return
				}
			}
		}
	}()
	return sock
}

func TestSessionRoutingAcrossProcesses(t *testing.T) {
	a, b := startRoutingEndpoint(t, "routing-a"), startRoutingEndpoint(t, "routing-b")
	if a.PID == b.PID || a.PID == os.Getpid() || b.PID == os.Getpid() {
		t.Fatal("gateway sessions must live in distinct child processes")
	}
	r := registrymemory.NewRegistry()
	for _, endpoint := range []routingEndpoint{a, b} {
		if err := r.Register(&registry.Service{Name: "websocket", Version: "routing-test",
			Nodes: []*registry.Node{{Id: endpoint.UID, Address: endpoint.RPC, Metadata: map[string]string{}}}}); err != nil {
			t.Fatal(err)
		}
	}
	routes := registryrouter.NewRouter(router.Registry(r), router.Cache())
	t.Cleanup(func() { _ = routes.Close() })
	c := grpcclient.NewClient(client.Router(routes), client.Lookup(network.Lookup),
		client.RequestTimeout(2*time.Second), client.Retries(0))
	rpc := pb.NewSessionService("websocket", c)
	sa, sb := openRoutingSocket(t, a), openRoutingSocket(t, b)
	if sa.sid == sb.sid {
		t.Fatal("gateway sessions must have distinct IDs")
	}
	send := func(uid, sid, body string) error {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_, err := rpc.Send(ctx, &pb.Message{Sid: sid, Route: "routing.push", Body: []byte(body)}, client.WithServerUid(uid))
		return err
	}
	receive := func(sock *routingSocket, body string) {
		t.Helper()
		select {
		case msg := <-sock.messages:
			if msg.Header["Micro-Route"] != "routing.push" || string(msg.Body) != body {
				t.Fatalf("unexpected push for %s: %+v", sock.sid, msg)
			}
		case err := <-sock.errors:
			t.Fatalf("websocket closed before receiving %q: %v", body, err)
		case <-time.After(3 * time.Second):
			t.Fatalf("push %q not received by session %s", body, sock.sid)
		}
	}
	for i := 0; i < 3; i++ {
		for _, target := range []struct {
			endpoint routingEndpoint
			socket   *routingSocket
		}{{a, sa}, {b, sb}} {
			body := fmt.Sprintf("%s-%d", target.endpoint.UID, i)
			if err := send(target.endpoint.UID, target.socket.sid, body); err != nil {
				t.Fatalf("directed push failed: %v", err)
			}
			receive(target.socket, body)
		}
	}
	for _, wrong := range []struct{ uid, sid string }{{a.UID, sb.sid}, {b.UID, sa.sid}} {
		if err := send(wrong.uid, wrong.sid, "wrong-instance"); err == nil || !strings.Contains(err.Error(), "session not found") {
			t.Fatalf("wrong instance must reject a foreign live SID: %v", err)
		}
	}
	_ = sa.conn.Close()
	deadline := time.Now().Add(3 * time.Second)
	for {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		_, err := rpc.Bind(ctx, &pb.SessionStatus{Sid: sa.sid}, client.WithServerUid(a.UID))
		cancel()
		if err != nil && strings.Contains(err.Error(), "session not found") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("closed session was not removed: %v", err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err := send(a.UID, sa.sid, "late-push"); err == nil || !strings.Contains(err.Error(), "session not found") {
		t.Fatalf("late push must fail after session closure: %v", err)
	}
	if err := send(b.UID, sb.sid, "still-active"); err != nil {
		t.Fatal(err)
	}
	receive(sb, "still-active")
	reconnected := openRoutingSocket(t, a)
	if reconnected.sid == sa.sid {
		t.Fatal("reconnected session reused the closed SID")
	}
	if err := send(a.UID, reconnected.sid, "reconnected"); err != nil {
		t.Fatal(err)
	}
	receive(reconnected, "reconnected")
	select {
	case msg := <-sb.messages:
		t.Fatalf("unrelated active instance received an extra push: %+v", msg)
	case <-time.After(100 * time.Millisecond):
	}
}
