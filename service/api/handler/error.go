package handler

import (
	"encoding/json"

	"github.com/micro/micro/v3/service/errors"
)

// LegacyErrorBody preserves the public HTTP error fields independently of the
// shared RPC error representation.
func LegacyErrorBody(err error) []byte {
	e := errors.FromError(err)
	body, _ := json.Marshal(struct {
		Id     string `json:"Id"`
		Code   int32  `json:"Code"`
		Detail string `json:"Detail"`
		Status string `json:"Status"`
	}{e.Id, e.Code, e.Detail, e.Status})
	return body
}
