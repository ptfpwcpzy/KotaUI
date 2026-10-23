package app

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/ptfpwcpzy/KotaUI/internal/config"
	"google.golang.org/grpc"
)

type statsTestServer struct {
	mu       sync.RWMutex
	counters []*statsCounter
}

func (s *statsTestServer) set(counters ...*statsCounter) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.counters = counters
}

func (s *statsTestServer) query(ctx context.Context, request *statsQueryRequest) (*statsQueryResponse, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*statsCounter, 0, len(s.counters))
	for _, counter := range s.counters {
		out = append(out, &statsCounter{Name: counter.Name, Value: counter.Value})
	}
	return &statsQueryResponse{Stats: out}, nil
}

type statsTestService interface {
	query(context.Context, *statsQueryRequest) (*statsQueryResponse, error)
}

func testQueryStatsHandler(srv any, ctx context.Context, decode func(any) error, interceptor grpc.UnaryServerInterceptor) (any, error) {
	request := &statsQueryRequest{}
	if err := decode(request); err != nil {
		return nil, err
	}
	call := func(ctx context.Context, request any) (any, error) {
		return srv.(statsTestService).query(ctx, request.(*statsQueryRequest))
	}
	if interceptor == nil {
		return call(ctx, request)
	}
	return interceptor(ctx, request, &grpc.UnaryServerInfo{Server: srv, FullMethod: statsQueryMethod}, call)
}

func startStatsTestServer(t *testing.T) (int, *statsTestServer, func()) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	service := &statsTestServer{}
	server := grpc.NewServer()
	server.RegisterService(&grpc.ServiceDesc{
		ServiceName: "v2ray.core.app.stats.command.StatsService",
		HandlerType: (*statsTestService)(nil),
		Methods:     []grpc.MethodDesc{{MethodName: "QueryStats", Handler: testQueryStatsHandler}},
	}, service)
	go func() { _ = server.Serve(listener) }()
	return listener.Addr().(*net.TCPAddr).Port, service, func() {
		server.Stop()
		_ = listener.Close()
	}
}

func TestSingBoxUserStatsIntegration(t *testing.T) {
	binary := os.Getenv("KOTAUI_SINGBOX_STATS_BIN")
	if binary == "" {
		t.Skip("set KOTAUI_SINGBOX_STATS_BIN to run against a compatible sing-box core")
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	_ = listener.Close()
	directory := t.TempDir()
	configPath := filepath.Join(directory, "config.json")
	body := fmt.Sprintf(`{"experimental":{"v2ray_api":{"listen":"127.0.0.1:%d","stats":{"enabled":true,"users":["alice"]}}},"inbounds":[],"outbounds":[{"type":"direct","tag":"direct"}]}`, port)
	if err := os.WriteFile(configPath, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	command := exec.Command(binary, "run", "-c", configPath)
	var logs bytes.Buffer
	command.Stdout = &logs
	command.Stderr = &logs
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = command.Process.Kill()
		_ = command.Wait()
	}()
	deadline := time.Now().Add(3 * time.Second)
	for {
		ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
		_, err = queryUserTraffic(ctx, port, []string{"alice"})
		cancel()
		if err == nil {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("query compatible sing-box stats service: %v; core logs: %s", err, logs.String())
		}
		time.Sleep(40 * time.Millisecond)
	}
}

func TestQueryUserTraffic(t *testing.T) {
	port, server, closeServer := startStatsTestServer(t)
	defer closeServer()
	server.set(
		&statsCounter{Name: "user>>>alice>>>traffic>>>uplink", Value: 11},
		&statsCounter{Name: "user>>>alice>>>traffic>>>downlink", Value: 29},
		&statsCounter{Name: "user>>>other>>>traffic>>>uplink", Value: 900},
	)
	traffic, err := queryUserTraffic(context.Background(), port, []string{"alice"})
	if err != nil {
		t.Fatal(err)
	}
	if got := traffic["alice"]; got.Upload != 11 || got.Download != 29 {
		t.Fatalf("unexpected traffic: %#v", got)
	}
}

func TestTrafficSyncAccumulatesAcrossCoreCounterReset(t *testing.T) {
	port, server, closeServer := startStatsTestServer(t)
	defer closeServer()
	a := testApp(t)
	a.runtime.StatsPort = port
	if err := a.store.Update(func(state *config.State) error {
		state.Clients = []config.Client{{ID: "alice", Username: "alice", Month: "2006-01", Credentials: map[string]string{}}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	server.set(&statsCounter{Name: "user>>>alice>>>traffic>>>uplink", Value: 12}, &statsCounter{Name: "user>>>alice>>>traffic>>>downlink", Value: 20})
	a.syncTraffic()
	first := a.store.Snapshot().Clients[0]
	if first.UploadBytes != 12 || first.DownloadBytes != 20 || first.UsedBytes != 32 {
		t.Fatalf("first sync: %#v", first)
	}
	server.set(&statsCounter{Name: "user>>>alice>>>traffic>>>uplink", Value: 18}, &statsCounter{Name: "user>>>alice>>>traffic>>>downlink", Value: 50})
	a.syncTraffic()
	second := a.store.Snapshot().Clients[0]
	if second.UploadBytes != 18 || second.DownloadBytes != 50 || second.UsedBytes != 68 {
		t.Fatalf("second sync: %#v", second)
	}
	server.set()
	a.syncTraffic()
	server.set(&statsCounter{Name: "user>>>alice>>>traffic>>>uplink", Value: 5}, &statsCounter{Name: "user>>>alice>>>traffic>>>downlink", Value: 6})
	a.syncTraffic()
	third := a.store.Snapshot().Clients[0]
	if third.UploadBytes != 23 || third.DownloadBytes != 56 || third.UsedBytes != 79 {
		t.Fatalf("counter reset sync: %#v", third)
	}
}

func TestTrafficSyncMakesOverLimitClientInactive(t *testing.T) {
	port, server, closeServer := startStatsTestServer(t)
	defer closeServer()
	a := testApp(t)
	a.runtime.StatsPort = port
	if err := a.store.Update(func(state *config.State) error {
		state.Clients = []config.Client{{ID: "alice", Username: "alice", Month: time.Now().Format("2006-01"), TotalLimitBytes: 30, Credentials: map[string]string{}}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	server.set(&statsCounter{Name: "user>>>alice>>>traffic>>>uplink", Value: 12}, &statsCounter{Name: "user>>>alice>>>traffic>>>downlink", Value: 20})
	a.syncTraffic()
	client := a.store.Snapshot().Clients[0]
	if client.UsedBytes != 32 || client.Active(time.Now()) {
		t.Fatalf("expected client to be over limit and inactive: %#v", client)
	}
}
