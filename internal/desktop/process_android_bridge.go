package desktop

import (
	"bufio"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net"
	"os"
	"runtime"
	"strings"
	"time"
)

// Companion IPC is loopback-only and authenticated before any UI data is sent.
// The token file is chosen locally; remote RPC cannot change backend or credentials.
func startAndroidBridge(ctx context.Context, cfg Config) (*process, error) {
	return startAndroidBridgeAddress(ctx, cfg, "127.0.0.1:28765")
}
func startAndroidBridgeAddress(ctx context.Context, cfg Config, address string) (*process, error) {
	if cfg.HelperPath != "" {
		return nil, errors.New("Android accessibility mode does not accept a helper executable")
	}
	token, err := readAndroidBridgeToken(cfg.AndroidBridgeTokenFile)
	if err != nil {
		return nil, err
	}
	conn, err := (&net.Dialer{Timeout: 5 * time.Second}).DialContext(ctx, "tcp", address)
	if err != nil {
		return nil, errors.New("Android companion is unavailable; enable local control in the companion app")
	}
	fail := func(err error) (*process, error) { conn.Close(); return nil, err }
	stop := context.AfterFunc(ctx, func() { conn.Close() })
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))

	reader := bufio.NewReaderSize(conn, 4096)
	var challenge struct {
		Protocol int    `json:"protocol"`
		Nonce    string `json:"nonce"`
	}
	line, err := reader.ReadSlice('\n')
	if err != nil || len(line) > 1024 || json.Unmarshal(line, &challenge) != nil || challenge.Protocol != 1 || len(challenge.Nonce) != 43 {
		stop()
		return fail(errors.New("invalid Android companion authentication challenge"))
	}
	if err = json.NewEncoder(conn).Encode(map[string]any{"protocol": 1, "proof": androidBridgeProof(token, "client:"+challenge.Nonce)}); err != nil {
		stop()
		return fail(errors.New("Android companion authentication interrupted"))
	}
	var auth struct {
		OK    bool   `json:"ok"`
		Proof string `json:"proof"`
	}
	line, err = reader.ReadSlice('\n')
	expected := androidBridgeProof(token, "server:"+challenge.Nonce)
	if err != nil || len(line) > 1024 || json.Unmarshal(line, &auth) != nil || !auth.OK || !hmac.Equal([]byte(auth.Proof), []byte(expected)) {
		stop()
		return fail(errors.New("Android companion pairing rejected"))
	}
	if !stop() || ctx.Err() != nil {
		return fail(ctx.Err())
	}
	_ = conn.SetDeadline(time.Time{})
	scan := bufio.NewScanner(reader)
	scan.Buffer(make([]byte, 65536), MaxMessage)
	p := &process{in: conn, out: scan}
	stopLifetime := context.AfterFunc(ctx, func() { conn.Close() })
	p.closeNative = func() { stopLifetime(); conn.Close() }
	return p, nil
}
func readAndroidBridgeToken(name string) (string, error) {
	if name == "" {
		return "", errors.New("accessibility mode requires android-bridge-token-file")
	}
	f, err := os.Open(name)
	if err != nil {
		return "", errors.New("cannot read local Android pairing token file")
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil || !st.Mode().IsRegular() || st.Size() > 128 || (runtime.GOOS != "windows" && st.Mode().Perm()&0077 != 0) {
		return "", errors.New("invalid Android pairing token file")
	}
	bytes, err := io.ReadAll(io.LimitReader(f, 129))
	if err != nil {
		return "", err
	}
	token := strings.TrimSpace(string(bytes))
	if len(token) != 43 {
		return "", errors.New("Android pairing token must contain 43 URL-safe characters")
	}
	for _, c := range token {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '-') {
			return "", errors.New("invalid Android pairing token")
		}
	}
	return token, nil
}
func androidBackend(cfg Config) (string, error) {
	switch cfg.AndroidBackend {
	case "", "auto":
		if cfg.AndroidBridgeTokenFile != "" {
			return "accessibility", nil
		}
		return "shell", nil
	case "shell":
		return "shell", nil
	case "accessibility":
		if cfg.AndroidBridgeTokenFile == "" {
			return "", errors.New("accessibility mode requires a pairing token file")
		}
		return "accessibility", nil
	default:
		return "", errors.New("android-backend must be auto, shell or accessibility")
	}
}

func androidBridgeProof(token, message string) string {
	mac := hmac.New(sha256.New, []byte(token))
	mac.Write([]byte(message))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}
