package api_test

import (
	"errors"
	"io"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/micro/micro/v3/service/api"
)

type failedFormReader struct{}

func (failedFormReader) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }
func (failedFormReader) Close() error             { return nil }

func TestFormPayloadReadError(t *testing.T) {
	r := httptest.NewRequest("POST", "/fixture/echo?query=value", nil)
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.Body = failedFormReader{}
	got, err := api.RequestPayload(r)
	if !errors.Is(err, io.ErrUnexpectedEOF) || got != nil {
		t.Fatalf("payload=%s error=%v; want original read error and no payload", got, err)
	}
}

func TestFormPayloadValidFields(t *testing.T) {
	r := httptest.NewRequest("POST", "/fixture/echo?tag=query&query=value", strings.NewReader("tag=body&tag=second&message=hello+world"))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded; charset=UTF-8")
	got, err := api.RequestPayload(r)
	if err != nil {
		t.Fatal(err)
	}
	assertJSONEqual(t, got, []byte(`{"tag":"body,second,query","query":"value","message":"hello world"}`))
}
