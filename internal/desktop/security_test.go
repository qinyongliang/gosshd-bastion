package desktop

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"
)

func TestTreePreservesHierarchyAndIndexes(t *testing.T) {
	tree := []Element{{Index: 0, Parent: -1, Role: "window", Name: "Example", Enabled: true}, {Index: 1, Parent: 0, Role: "group", Name: "Form", Enabled: true}, {Index: 2, Parent: 1, Role: "edit", Name: "Name", Value: "sample", Enabled: true, Actions: []string{"set_value"}}}
	if err := ValidateElements(tree); err != nil {
		t.Fatal(err)
	}
	text := RenderTree(tree, false)
	if !strings.Contains(text, "    [2] edit") || !strings.Contains(text, "actions=set_value") {
		t.Fatalf("invalid rendered hierarchy %s", text)
	}
	tree[2].Parent = 2
	if ValidateElements(tree) == nil {
		t.Fatal("hierarchy cycle accepted")
	}
}
func TestCloseCancelsOpeningSession(t *testing.T) {
	started := make(chan struct{})
	svc := NewService(Adapter{Authorize: func(context.Context, string, string, bool) error { return nil }, Dial: func(ctx context.Context, _ string) (io.ReadWriteCloser, error) {
		close(started)
		<-ctx.Done()
		return nil, ctx.Err()
	}})
	done := make(chan struct{})
	go func() { defer close(done); _, _, _ = svc.Open(context.Background(), "actor", "target") }()
	<-started
	svc.Close()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("opening operation survived service close")
	}
	if _, _, err := svc.Open(context.Background(), "actor", "target"); err == nil {
		t.Fatal("closed service accepted an open")
	}
}
func TestAuditFailureStopsControlBeforeDispatch(t *testing.T) {
	svc, _, _ := testService(t)
	info, _, err := svc.Open(context.Background(), "actor", "target")
	if err != nil {
		t.Fatal(err)
	}
	svc.adapter.Audit = func(context.Context, Event) error { return errors.New("database unavailable") }
	if _, err = svc.Call(context.Background(), "actor", info.ID, Request{Method: "launch_app", Params: Params{App: "/example.app"}}); err == nil || !strings.Contains(err.Error(), "audit unavailable") {
		t.Fatal("control bypassed failed audit")
	}
}
func TestQueuedCallCanBeCancelled(t *testing.T) {
	svc, _, _ := testService(t)
	info, _, err := svc.Open(context.Background(), "actor", "target")
	if err != nil {
		t.Fatal(err)
	}
	svc.mu.Lock()
	item := svc.sessions[info.ID]
	svc.mu.Unlock()
	<-item.gate
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	started := time.Now()
	_, err = svc.Call(ctx, "actor", info.ID, Request{Method: "status"})
	item.gate <- struct{}{}
	if err == nil || time.Since(started) > time.Second {
		t.Fatal("queued call ignored cancellation")
	}
}

func TestCloseSessionIsAuditedAndCleanupSurvivesAuditFailure(t *testing.T) {
	svc, _, events := testService(t)
	info, _, err := svc.Open(context.Background(), "actor", "target")
	if err != nil {
		t.Fatal(err)
	}
	if err = svc.CloseSession(context.Background(), "actor", info.ID); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, event := range *events {
		if event.Method == "session_close" && event.SessionID == info.ID {
			found = true
		}
	}
	if !found {
		t.Fatal("session close audit missing")
	}
	info, _, err = svc.Open(context.Background(), "actor", "target")
	if err != nil {
		t.Fatal(err)
	}
	svc.adapter.Audit = func(context.Context, Event) error { return errors.New("audit unavailable") }
	if err = svc.CloseSession(context.Background(), "actor", info.ID); err == nil {
		t.Fatal("audit failure not reported")
	}
	svc.mu.Lock()
	remaining := len(svc.sessions)
	svc.mu.Unlock()
	if remaining != 0 {
		t.Fatal("audit failure prevented cleanup")
	}
}

func TestCompactTreePreservesSemanticStateWithoutGeometry(t *testing.T) {
	selected, expanded, scrollable := true, false, true
	minimum, maximum, percent := 0.0, 100.0, 50.0
	elements := []Element{
		{Index: 0, Parent: -1, Role: "ControlType.Window", Name: "Example", Enabled: true},
		{Index: 1, Parent: 0, Role: "ControlType.CheckBox", Name: "Option", Enabled: true, Selected: &selected, Expanded: &expanded, ToggleState: "on", Minimum: &minimum, Maximum: &maximum, VerticalScrollable: &scrollable, VerticalScrollPercent: &percent, Bounds: Bounds{X: 123, Y: 456}, Actions: []string{"toggle"}},
	}
	text := RenderTree(elements, true)
	for _, want := range []string{`  [1] CheckBox "Option"`, "selected=true", "expanded=false", "toggle=on", "min=0", "max=100", "vertical_scrollable=true", "vertical_scroll_percent=50", "actions=toggle", "increase max_depth/max_elements"} {
		if !strings.Contains(text, want) {
			t.Errorf("compact tree lost %q: %s", want, text)
		}
	}
	if strings.Contains(text, "ControlType.") || strings.Contains(text, "123") || strings.Contains(text, "456") {
		t.Fatalf("redundant role prefix or geometry: %s", text)
	}
}
