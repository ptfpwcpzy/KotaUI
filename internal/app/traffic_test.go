package app

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
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

func TestSingBoxSocksUserStatsIntegration(t *testing.T) {
	binary := os.Getenv("KOTAUI_SINGBOX_STATS_BIN")
	if binary == "" {
		t.Skip("set KOTAUI_SINGBOX_STATS_BIN to run against a compatible sing-box core")
	}
	httpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("hello"))
	}))
	defer httpServer.Close()
	target := strings.TrimPrefix(httpServer.URL, "http://")
	host, portStr, err := net.SplitHostPort(target)
	if err != nil {
		t.Fatal(err)
	}
	targetPort, err := strconv.Atoi(portStr)
	if err != nil {
		t.Fatal(err)
	}
	statsListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	statsPort := statsListener.Addr().(*net.TCPAddr).Port
	_ = statsListener.Close()
	socksListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	socksPort := socksListener.Addr().(*net.TCPAddr).Port
	_ = socksListener.Close()
	directory := t.TempDir()
	configPath := filepath.Join(directory, "config.json")
	body := fmt.Sprintf(`{"experimental":{"v2ray_api":{"listen":"127.0.0.1:%d","stats":{"enabled":true,"users":["relay-abc"]}}},"inbounds":[{"type":"socks","tag":"socks-in","listen":"127.0.0.1","listen_port":%d,"users":[{"username":"relay-abc","password":"secret"}]}],"outbounds":[{"type":"direct","tag":"direct"}],"route":{"final":"direct"}}`, statsPort, socksPort)
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
	deadline := time.Now().Add(5 * time.Second)
	for {
		ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
		_, err = queryUserTraffic(ctx, statsPort, []string{"relay-abc"})
		cancel()
		if err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("query compatible sing-box stats service: %v; core logs: %s", err, logs.String())
		}
		time.Sleep(40 * time.Millisecond)
	}
	conn, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", socksPort))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
	writeSocks := func(data ...byte) {
		t.Helper()
		if _, err := conn.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	readSocks := func(n int) []byte {
		t.Helper()
		buf := make([]byte, n)
		if _, err := io.ReadFull(conn, buf); err != nil {
			t.Fatal(err)
		}
		return buf
	}
	writeSocks(0x05, 0x01, 0x02)
	if reply := readSocks(2); reply[0] != 0x05 || reply[1] != 0x02 {
		t.Fatalf("unexpected socks greeting reply: %v", reply)
	}
	auth := []byte{0x01, byte(len("relay-abc"))}
	auth = append(auth, []byte("relay-abc")...)
	auth = append(auth, byte(len("secret")))
	auth = append(auth, []byte("secret")...)
	writeSocks(auth...)
	if reply := readSocks(2); reply[0] != 0x01 || reply[1] != 0x00 {
		t.Fatalf("unexpected socks auth reply: %v", reply)
	}
	ip := net.ParseIP(host).To4()
	request := []byte{0x05, 0x01, 0x00, 0x01, ip[0], ip[1], ip[2], ip[3], byte(targetPort >> 8), byte(targetPort)}
	writeSocks(request...)
	reply := readSocks(4)
	if reply[0] != 0x05 || reply[1] != 0x00 {
		t.Fatalf("unexpected socks connect reply: %v", reply)
	}
	if reply[3] == 0x01 {
		readSocks(6)
	} else if reply[3] == 0x03 {
		length := readSocks(1)[0]
		readSocks(int(length) + 2)
	} else {
		readSocks(18)
	}
	if _, err := conn.Write([]byte("GET / HTTP/1.0\r\nHost: local\r\n\r\n")); err != nil {
		t.Fatal(err)
	}
	response, err := io.ReadAll(conn)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(response, []byte("hello")) {
		t.Fatalf("unexpected response through socks: %q; core logs: %s", response, logs.String())
	}
	traffic, err := queryUserTraffic(context.Background(), statsPort, []string{"relay-abc"})
	if err != nil {
		t.Fatal(err)
	}
	counters := traffic["relay-abc"]
	if counters.Upload <= 0 || counters.Download <= 0 {
		t.Fatalf("expected socks user traffic to be counted, got %#v", counters)
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

func TestTrafficSyncCountsLandingRelayUser(t *testing.T) {
	port, server, closeServer := startStatsTestServer(t)
	defer closeServer()
	a := testApp(t)
	a.runtime.StatsPort = port
	if err := a.store.Update(func(state *config.State) error {
		state.Settings.Relay = config.RelaySettings{Enabled: true, Role: "landing", Port: 41234, Username: "relay-abc", Password: "secret"}
		state.Clients = []config.Client{}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	server.set(&statsCounter{Name: "user>>>relay-abc>>>traffic>>>uplink", Value: 100}, &statsCounter{Name: "user>>>relay-abc>>>traffic>>>downlink", Value: 50})
	a.syncTraffic()
	relay := a.store.Snapshot().RelayTraffic
	if relay.UsedBytes != 150 || relay.MonthlyUsedBytes != 150 || relay.DailyUsedBytes != 150 {
		t.Fatalf("relay traffic after first sync: %#v", relay)
	}
	server.set(&statsCounter{Name: "user>>>relay-abc>>>traffic>>>uplink", Value: 130}, &statsCounter{Name: "user>>>relay-abc>>>traffic>>>downlink", Value: 70})
	a.syncTraffic()
	relay = a.store.Snapshot().RelayTraffic
	if relay.UsedBytes != 200 || relay.MonthlyUsedBytes != 200 || relay.DailyUsedBytes != 200 {
		t.Fatalf("relay traffic after second sync: %#v", relay)
	}
	rows := dailyTrafficByUser(a.store.Snapshot(), time.Now())
	last := rows[len(rows)-1]
	users, _ := last["users"].([]map[string]any)
	if len(users) != 1 || users[0]["username"] != "中转主机A" || users[0]["bytes"] != int64(200) {
		t.Fatalf("history last row users = %#v", users)
	}
}

func TestNextRelayRuleSetMarker(t *testing.T) {
	cases := []struct {
		applied, marker, want string
		changed               bool
	}{
		{"", "", "", false},
		{"", "1700000000", "1700000000", false},
		{"1700000000", "1700000000", "1700000000", false},
		{"1700000000", "1700086400", "1700086400", true},
		{"1700086400", "", "1700086400", false},
	}
	for _, c := range cases {
		got, changed := nextRelayRuleSetMarker(c.applied, c.marker)
		if got != c.want || changed != c.changed {
			t.Fatalf("nextRelayRuleSetMarker(%q, %q) = (%q, %v), want (%q, %v)", c.applied, c.marker, got, changed, c.want, c.changed)
		}
	}
}

func TestDailyTrafficByUserCoversMonthDays(t *testing.T) {
	cases := []struct {
		now  string
		want int
	}{
		{"2026-10-23T12:00:00Z", 31},
		{"2026-02-15T12:00:00Z", 28},
		{"2024-02-15T12:00:00Z", 29},
		{"2026-04-15T12:00:00Z", 30},
	}
	for _, c := range cases {
		now, err := time.Parse(time.RFC3339, c.now)
		if err != nil {
			t.Fatal(err)
		}
		rows := dailyTrafficByUser(config.State{}, now)
		if len(rows) != c.want {
			t.Fatalf("%s: got %d rows, want %d", c.now, len(rows), c.want)
		}
		last := now.In(config.PanelLocation).Format("2006-01-02")
		if rows[len(rows)-1]["date"] != last {
			t.Fatalf("%s: last row date = %v, want %s", c.now, rows[len(rows)-1]["date"], last)
		}
	}
}
