package app

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func terminalWSURL(srv *httptest.Server) string {
	return "ws" + strings.TrimPrefix(srv.URL, "http") + "/api/terminal"
}

func TestTerminalRequiresLogin(t *testing.T) {
	a := testApp(t)
	srv := httptest.NewServer(a.Handler())
	defer srv.Close()
	_, resp, err := websocket.DefaultDialer.Dial(terminalWSURL(srv), nil)
	if err == nil {
		t.Fatal("unauthenticated dial must fail")
	}
	if resp == nil || resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %v", resp)
	}
}

func TestTerminalRunsCommandsAndExits(t *testing.T) {
	a := testApp(t)
	srv := httptest.NewServer(a.Handler())
	defer srv.Close()
	header := http.Header{}
	c := login(t, a)
	header.Set("Cookie", c.Name+"="+c.Value)
	conn, _, err := websocket.DefaultDialer.Dial(terminalWSURL(srv), header)
	if err != nil {
		t.Fatalf("dial terminal: %v", err)
	}
	defer conn.Close()

	// Resize first: a bad resize handler would break everything after it.
	if err := conn.WriteMessage(websocket.BinaryMessage, []byte(`{"cols":80,"rows":24}`)); err != nil {
		t.Fatal(err)
	}
	// The marker only appears if the shell really evaluates the command:
	// the typed input contains "6*7", the output contains "42".
	marker := "TERMINAL_OK_42"
	if err := conn.WriteMessage(websocket.TextMessage, []byte("echo TERMINAL_OK_$((6*7))\n")); err != nil {
		t.Fatal(err)
	}
	var got strings.Builder
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
		_, msg, err := conn.ReadMessage()
		if err != nil {
			t.Fatalf("read: %v (got so far: %q)", err, got.String())
		}
		got.Write(msg)
		if strings.Contains(got.String(), marker) {
			break
		}
	}
	if !strings.Contains(got.String(), marker) {
		t.Fatalf("marker %q not in output %q", marker, got.String())
	}

	// Exiting the shell must close the connection with an exit notice.
	if err := conn.WriteMessage(websocket.TextMessage, []byte("exit\n")); err != nil {
		t.Fatal(err)
	}
	_ = conn.SetReadDeadline(time.Now().Add(10 * time.Second))
	for {
		_, msg, err := conn.ReadMessage()
		if err != nil {
			break
		}
		got.Write(msg)
	}
	if !strings.Contains(got.String(), "[终端已退出]") {
		t.Fatalf("expected exit notice, got %q", got.String())
	}
}
