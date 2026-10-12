package server

import "testing"

func TestHTTPForwardingUIE2EWithBrowser(t *testing.T) {
	testUIE2EWithBrowser(t, false, "http_forwarding.test.mjs")
}

func TestPortForwardingUIE2EWithBrowser(t *testing.T) {
	testUIE2EWithBrowser(t, false, "port_forwarding.test.mjs")
}
