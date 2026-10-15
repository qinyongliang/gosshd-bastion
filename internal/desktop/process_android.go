//go:build android

package desktop

import (
	"context"
	androidhelper "github.com/qinyongliang/gosshd-bastion/internal/desktop/native/android"
)

func startNativeProcess(ctx context.Context, cfg Config) (*process, error) {
	backend, err := androidBackend(cfg)
	if err != nil {
		return nil, err
	}
	if backend == "accessibility" {
		return startAndroidBridge(ctx, cfg)
	}
	return startEmbeddedProcess(ctx, cfg, androidhelper.NewHelper())
}
