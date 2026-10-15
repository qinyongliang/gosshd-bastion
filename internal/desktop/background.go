package desktop

import (
	"encoding/json"
	"errors"
	"github.com/qinyongliang/gosshd-bastion/internal/desktop/contract"
)

const BackgroundNotSupported = contract.BackgroundNotSupported

type ProtocolError = contract.ProtocolError

func backgroundUnsupported(reason string) error {
	return &ProtocolError{Code: BackgroundNotSupported, Message: reason}
}
func errorResponse(id string, err error) Response {
	res := Response{ID: id, Error: err.Error()}
	var failure *ProtocolError
	if errors.As(err, &failure) {
		res.ErrorCode = failure.Code
	}
	return res
}
func supportsBackground(result json.RawMessage) bool {
	var status struct {
		Supported bool `json:"background_supported"`
	}
	return json.Unmarshal(result, &status) == nil && status.Supported
}
