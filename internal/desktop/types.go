// Package desktop is an independent desktop RPC/MCP module. Native backends
// use private RPC streams; this package knows nothing about GOSSHD users or storage.
package desktop

import "github.com/qinyongliang/gosshd-bastion/internal/desktop/contract"

// The wire contract lives in the leaf "contract" package so the embedded
// Android helper can share it without importing this module. These aliases
// keep the module's public surface unchanged.
const (
	MaxMessage = contract.MaxMessage
	MaxImage   = contract.MaxImage
)

type (
	Window    = contract.Window
	Bounds    = contract.Bounds
	Element   = contract.Element
	Image     = contract.Image
	ImageInfo = contract.ImageInfo
	State     = contract.State
	Params    = contract.Params
	Request   = contract.Request
	Response  = contract.Response
)

func IsRead(method string) bool        { return contract.IsRead(method) }
func IsControl(method string) bool     { return contract.IsControl(method) }
func NeedsSnapshot(method string) bool { return contract.NeedsSnapshot(method) }
func Validate(req Request) error       { return contract.Validate(req) }
