package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

func TestBasicCall(t *testing.T) {
	if v := os.Getenv("IN_TRAVIS_CI"); v == "yes" {
		return
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/helloworld/call" {
			t.Fatalf("expected /helloworld/call, got %s", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
			t.Fatalf("expected bearer token, got %q", got)
		}
		var req map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatal(err)
		}
		if req["name"] != "Alice" {
			t.Fatalf("expected request name Alice, got %v", req["name"])
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{}`))
	}))
	defer server.Close()

	response := map[string]interface{}{}
	client := NewClient(&Options{Token: "test-token"})
	client.SetAddress(server.URL)
	if err := client.Call("helloworld", "call", map[string]interface{}{
		"name": "Alice",
	}, &response); err != nil {
		t.Fatal(err)
	}
	if len(response) > 0 {
		t.Fatal(len(response))
	}
}
