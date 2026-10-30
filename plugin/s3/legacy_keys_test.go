package s3

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/micro/micro/v3/service/store"
	"github.com/stretchr/testify/require"
)

func TestLegacyKeyFormatPreservesOldServerObjects(t *testing.T) {
	for _, bucket := range []string{"", "artifacts"} {
		t.Run("bucket="+bucket, func(t *testing.T) {
			const key = "build://server:v1.2.3"
			prefix := "/micro/"
			if bucket != "" {
				prefix = "/artifacts/micro/"
			}
			legacyPath := prefix + "build-server-v1-2-3"
			currentPath := prefix + "build/server/v1.2.3"
			objects := map[string]string{legacyPath: "old-server", currentPath: "unrelated-current-format"}
			var requests []string
			blob := newTestBlobStore(t, func(req *http.Request) (*http.Response, error) {
				requests = append(requests, req.Method+" "+req.URL.Path)
				switch req.Method {
				case http.MethodGet:
					value, ok := objects[req.URL.Path]
					if !ok {
						return s3Response(req, 404, "<Error><Code>NoSuchKey</Code></Error>"), nil
					}
					return s3Response(req, 200, value), nil
				case http.MethodPut:
					if req.URL.Path == "/micro" {
						return s3Response(req, 200, ""), nil
					}
					value, err := io.ReadAll(req.Body)
					require.NoError(t, err)
					objects[req.URL.Path] = string(value)
					return s3Response(req, 200, ""), nil
				case http.MethodDelete:
					delete(objects, req.URL.Path)
					return s3Response(req, 204, ""), nil
				default:
					t.Fatalf("unexpected request: %s", req.Method)
					return nil, nil
				}
			})
			LegacyKeyFormat()(blob.options)
			blob.options.Bucket = bucket
			read := func(expected string) {
				body, err := blob.Read(key, store.BlobNamespace("micro"))
				require.NoError(t, err)
				data, err := io.ReadAll(body)
				require.NoError(t, err)
				require.Equal(t, expected, string(data))
			}
			read("old-server")
			require.NoError(t, blob.Write(key, strings.NewReader("new-server"), store.BlobNamespace("micro")))
			require.Equal(t, "new-server", objects[legacyPath], "old server must see new writes at its original key")
			require.Equal(t, "unrelated-current-format", objects[currentPath])
			objects[legacyPath] = "old-server-overwrite"
			read("old-server-overwrite")
			require.NoError(t, blob.Delete(key, store.BlobNamespace("micro")))
			require.NotContains(t, objects, legacyPath)
			require.Equal(t, "unrelated-current-format", objects[currentPath])
			_, err := blob.Read(key, store.BlobNamespace("micro"))
			require.Error(t, err, "legacy mode must not revive another format")
			for _, request := range requests {
				require.NotContains(t, request, currentPath)
			}
		})
	}
}

func TestNewBlobStoreDoesNotWriteProbeObject(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { requests.Add(1); w.WriteHeader(403) }))
	defer server.Close()
	blob, err := NewBlobStore(Endpoint(server.URL), Region("us-east-1"), Credentials("fixture", "fixture"), Insecure())
	require.NoError(t, err)
	require.NotNil(t, blob)
	require.Zero(t, requests.Load())
}
