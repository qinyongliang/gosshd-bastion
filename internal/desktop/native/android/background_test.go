package android

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/qinyongliang/gosshd-bastion/internal/desktop/contract"
)

func TestAndroidStrictBackgroundRefusesBeforeDeviceTools(t *testing.T) {
	h := &Helper{UID: 0, Lookup: func(string) (string, error) { t.Fatal("strict refusal reached a device tool"); return "", nil }}
	index := 0
	for _, method := range []string{"click", "scroll", "set_value", "perform_action", "get_window_state", "screenshot", "activate_window", "type_text", "press_key", "drag", "launch_app", "request_permissions"} {
		p := contract.Params{Mode: "background", Window: &contract.Window{ID: "window", App: "com.example.fixture"}, SnapshotID: "snapshot", ElementIndex: &index, Action: "invoke", Text: "sample", Key: "a", App: "com.example.fixture"}
		_, err := h.Dispatch(context.Background(), method, p)
		var failure *contract.ProtocolError
		if !errors.As(err, &failure) || failure.Code != contract.BackgroundNotSupported {
			t.Fatalf("%s: coded refusal missing: %v", method, err)
		}
	}
}
func TestAndroidNativeWirePreservesBackgroundErrorCode(t *testing.T) {
	h := &Helper{UID: 0}
	var output strings.Builder
	request := `{"id":"refusal","method":"click","params":{"mode":"background","window":{"id":"window","app":"com.example.fixture"},"snapshot_id":"s","element_index":0}}` + "\n"
	if err := Serve(context.Background(), strings.NewReader(request), &output, h); err != nil {
		t.Fatal(err)
	}
	var response contract.Response
	if err := json.Unmarshal([]byte(output.String()), &response); err != nil || response.OK || response.ErrorCode != contract.BackgroundNotSupported {
		t.Fatalf("native wire response: %+v %v", response, err)
	}
}
