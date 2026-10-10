package tunnel

import "testing"

func TestFileOperationOnlyFromTrustedRelay(t *testing.T) {
	c := NewConn(nil)
	called := 0
	c.FileOperationHandler = func(body []byte) { called++ }
	op := Packet{Kind: FileOperation, Body: []byte(`{"upload":{"path":"unauthorized"}}`)}
	c.receive(op, true)
	if called != 0 {
		t.Fatal("direct peer injected a file operation")
	}
	c.receive(op, false)
	if called != 1 {
		t.Fatal("trusted relay operation was not delivered")
	}
}
