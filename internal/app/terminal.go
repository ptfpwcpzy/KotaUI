package app

import (
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"time"

	"github.com/creack/pty"
	"github.com/gorilla/websocket"
)

// maxConcurrentTerminals caps simultaneous terminal shells.
const maxConcurrentTerminals = 4

var terminalUpgrader = websocket.Upgrader{
	ReadBufferSize:  4096,
	WriteBufferSize: 4096,
}

type terminalResize struct {
	Cols int `json:"cols"`
	Rows int `json:"rows"`
}

func (a *App) terminalSlot() bool {
	a.terminalMu.Lock()
	defer a.terminalMu.Unlock()
	if a.terminalCount >= maxConcurrentTerminals {
		return false
	}
	a.terminalCount++
	return true
}

func (a *App) releaseTerminalSlot() {
	a.terminalMu.Lock()
	defer a.terminalMu.Unlock()
	if a.terminalCount > 0 {
		a.terminalCount--
	}
}

func terminalShell() string {
	if _, err := os.Stat("/bin/bash"); err == nil {
		return "/bin/bash"
	}
	return "/bin/sh"
}

func terminalWorkDir() string {
	if st, err := os.Stat("/root"); err == nil && st.IsDir() {
		return "/root"
	}
	return "/"
}

// terminal upgrades an authenticated session to a websocket carrying a local
// root shell. Text frames from the client are keystrokes; binary frames are
// JSON resize messages {"cols":N,"rows":N}. Shell output is sent back as
// binary frames. The shell dies when the websocket closes.
func (a *App) terminal(w http.ResponseWriter, r *http.Request) {
	if !a.terminalSlot() {
		http.Error(w, "终端数量已达上限", http.StatusServiceUnavailable)
		return
	}
	defer a.releaseTerminalSlot()

	conn, err := terminalUpgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()

	cmd := exec.Command(terminalShell())
	cmd.Env = append(os.Environ(), "TERM=xterm-256color")
	cmd.Dir = terminalWorkDir()
	ptmx, err := pty.Start(cmd)
	if err != nil {
		return
	}
	defer func() {
		_ = cmd.Process.Kill()
		_ = ptmx.Close()
	}()
	_ = pty.Setsize(ptmx, &pty.Winsize{Cols: 80, Rows: 24})

	conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	conn.SetPongHandler(func(string) error {
		conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		return nil
	})
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}()

	// Shell output -> websocket.
	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := ptmx.Read(buf)
			if n > 0 {
				conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
				if err := conn.WriteMessage(websocket.BinaryMessage, buf[:n]); err != nil {
					return
				}
			}
			if err != nil {
				return
			}
		}
	}()

	// Shell exit -> tell the client, then close.
	go func() {
		_ = cmd.Wait()
		conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
		_ = conn.WriteMessage(websocket.TextMessage, []byte("\r\n[终端已退出]\r\n"))
		conn.Close()
	}()

	// Websocket -> shell.
	for {
		mt, msg, err := conn.ReadMessage()
		if err != nil {
			return
		}
		switch mt {
		case websocket.TextMessage:
			if _, err := ptmx.Write(msg); err != nil {
				return
			}
		case websocket.BinaryMessage:
			var rz terminalResize
			if json.Unmarshal(msg, &rz) == nil && rz.Cols > 0 && rz.Rows > 0 {
				_ = pty.Setsize(ptmx, &pty.Winsize{Cols: uint16(rz.Cols), Rows: uint16(rz.Rows)})
			}
		}
	}
}
