package app

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os/exec"
	"sort"
	"strings"
	"time"

	"github.com/ptfpwcpzy/KotaUI/internal/config"
	"github.com/ptfpwcpzy/KotaUI/internal/proxy"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

const statsQueryMethod = "/v2ray.core.app.stats.command.StatsService/QueryStats"

type statsQueryRequest struct {
	Patterns []string `protobuf:"bytes,3,rep,name=patterns,proto3"`
	Regexp   bool     `protobuf:"varint,4,opt,name=regexp,proto3"`
}

func (*statsQueryRequest) Reset()         {}
func (*statsQueryRequest) String() string { return "" }
func (*statsQueryRequest) ProtoMessage()  {}

type statsCounter struct {
	Name  string `protobuf:"bytes,1,opt,name=name,proto3"`
	Value int64  `protobuf:"varint,2,opt,name=value,proto3"`
}

func (*statsCounter) Reset()         {}
func (*statsCounter) String() string { return "" }
func (*statsCounter) ProtoMessage()  {}

type statsQueryResponse struct {
	Stats []*statsCounter `protobuf:"bytes,1,rep,name=stat,proto3"`
}

func (*statsQueryResponse) Reset()         {}
func (*statsQueryResponse) String() string { return "" }
func (*statsQueryResponse) ProtoMessage()  {}

func queryUserTraffic(ctx context.Context, port int, usernames []string) (map[string]config.TrafficCounters, error) {
	if port < 1 || port > 65535 {
		return nil, errors.New("流量统计端口无效")
	}
	if len(usernames) == 0 {
		return map[string]config.TrafficCounters{}, nil
	}
	address := fmt.Sprintf("127.0.0.1:%d", port)
	dialer := net.Dialer{}
	connection, err := grpc.DialContext(ctx, address, grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
		return dialer.DialContext(ctx, "tcp", address)
	}), grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithBlock())
	if err != nil {
		return nil, err
	}
	defer connection.Close()
	response := &statsQueryResponse{}
	request := &statsQueryRequest{Patterns: []string{"user>>>"}, Regexp: false}
	if err := connection.Invoke(ctx, statsQueryMethod, request, response); err != nil {
		return nil, err
	}
	wanted := make(map[string]struct{}, len(usernames))
	for _, username := range usernames {
		wanted[username] = struct{}{}
	}
	traffic := make(map[string]config.TrafficCounters, len(usernames))
	for _, stat := range response.Stats {
		username, direction, ok := parseUserTrafficCounter(stat.Name)
		if !ok || stat.Value < 0 {
			continue
		}
		if _, exists := wanted[username]; !exists {
			continue
		}
		value := traffic[username]
		if direction == "uplink" {
			value.Upload = stat.Value
		} else {
			value.Download = stat.Value
		}
		traffic[username] = value
	}
	return traffic, nil
}

func parseUserTrafficCounter(name string) (string, string, bool) {
	parts := strings.Split(name, ">>>")
	if len(parts) != 4 || parts[0] != "user" || parts[2] != "traffic" || (parts[3] != "uplink" && parts[3] != "downlink") || parts[1] == "" {
		return "", "", false
	}
	return parts[1], parts[3], true
}

func trafficDelta(current, previous int64) int64 {
	if current <= previous {
		if current < previous {
			return current
		}
		return 0
	}
	return current - previous
}

func (a *App) syncTraffic() {
	state := a.store.Snapshot()
	if len(state.Clients) == 0 {
		return
	}
	usernames := make([]string, 0, len(state.Clients))
	for _, client := range state.Clients {
		usernames = append(usernames, client.Username)
	}
	sort.Strings(usernames)
	ctx, cancel := context.WithTimeout(context.Background(), 1200*time.Millisecond)
	defer cancel()
	current, err := queryUserTraffic(ctx, a.runtime.StatsPort, usernames)
	if err != nil {
		return
	}
	nowTime := time.Now()
	month := nowTime.Format("2006-01")
	needsSave := len(state.TrafficCounters) != len(state.Clients)
	if !needsSave {
		for _, client := range state.Clients {
			if client.Month != month || state.TrafficCounters[client.Username] != current[client.Username] {
				needsSave = true
				break
			}
		}
	}
	if !needsSave {
		return
	}
	reloadCore := false
	_ = a.store.Update(func(next *config.State) error {
		if next.TrafficCounters == nil {
			next.TrafficCounters = map[string]config.TrafficCounters{}
		}
		for i := range next.Clients {
			client := &next.Clients[i]
			wasActive := client.Active(nowTime)
			if client.Month != month {
				client.Month = month
				client.MonthlyUsedBytes = 0
			}
			now := current[client.Username]
			before := next.TrafficCounters[client.Username]
			upload := trafficDelta(now.Upload, before.Upload)
			download := trafficDelta(now.Download, before.Download)
			if delta := upload + download; delta > 0 {
				client.UploadBytes += upload
				client.DownloadBytes += download
				client.UsedBytes += delta
				client.MonthlyUsedBytes += delta
			}
			next.TrafficCounters[client.Username] = now
			if wasActive != client.Active(nowTime) {
				reloadCore = true
			}
		}
		for username := range next.TrafficCounters {
			found := false
			for _, client := range next.Clients {
				if client.Username == username {
					found = true
					break
				}
			}
			if !found {
				delete(next.TrafficCounters, username)
			}
		}
		return nil
	})
	if reloadCore {
		if err := proxy.Write(a.store.Snapshot(), a.runtime); err == nil && a.runtime.ManageSingBox && filePresent(a.runtime.SingBoxBin) {
			if hasSystemd() {
				_ = exec.Command("systemctl", "restart", "kotaui-singbox").Run()
			} else {
				_ = exec.Command("rc-service", "kotaui-singbox", "restart").Run()
			}
		}
	}
}
