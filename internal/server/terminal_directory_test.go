package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestTerminalDirectoryIntegrationAcrossEveryFrameBoundary(t *testing.T) {
	for _, cwd := range []string{"/home/user/work;中文", `C:\Users\user\3D`, "/D:/3D"} {
		frame := "\x1b]633;P;Cwd=" + cwd + "\a"
		for split := 1; split < len(frame); split++ {
			session := &terminalSession{screen: newTerminalScreenBuffer(24), clients: map[*terminalWSWriter]bool{}}
			session.writeOutput("output", []byte("before"+frame[:split]))
			session.writeOutput("output", []byte(frame[split:]+"after"))
			if session.currentDirectory != cwd {
				t.Fatalf("split %d: cwd = %q, want %q", split, session.currentDirectory, cwd)
			}
			if got := session.output.String(); got != "beforeafter" {
				t.Fatalf("split %d: output = %q", split, got)
			}
		}
	}
}

func TestTerminalDirectoryReplayAndChanges(t *testing.T) {
	session := &terminalSession{screen: newTerminalScreenBuffer(24), clients: map[*terminalWSWriter]bool{}}
	session.writeOutput("output", []byte("welcome\x1b]633;P;Cwd=/home/user\a"))
	attached := make(chan struct{})
	done := make(chan struct{})
	upgrader := websocket.Upgrader{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ws, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer ws.Close()
		writer := &terminalWSWriter{ws: ws}
		session.attach(writer)
		close(attached)
		<-done
		session.detach(writer)
	}))
	defer srv.Close()
	defer close(done)
	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(srv.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	<-attached
	var msg terminalWSMessage
	if err := conn.ReadJSON(&msg); err != nil || msg.Type != "output" || msg.Data != "welcome" {
		t.Fatalf("output replay = %+v, %v", msg, err)
	}
	if err := conn.ReadJSON(&msg); err != nil || msg.Type != "cwd" || msg.Data != "/home/user" {
		t.Fatalf("cwd replay = %+v, %v", msg, err)
	}
	session.writeOutput("output", []byte("\x1b]633;P;Cwd=/home/user\a\x1b]633;P;Cwd=/home/user/work\a"))
	if err := conn.ReadJSON(&msg); err != nil || msg.Type != "cwd" || msg.Data != "/home/user/work" {
		t.Fatalf("cwd update = %+v, %v", msg, err)
	}
	session.writeOutput("output", []byte("marker"))
	if err := conn.ReadJSON(&msg); err != nil || msg.Type != "output" || msg.Data != "marker" {
		t.Fatalf("duplicate cwd event: %+v, %v", msg, err)
	}
}
