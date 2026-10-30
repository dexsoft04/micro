package handler

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"

	pb "github.com/micro/micro/v3/proto/store"
	"github.com/micro/micro/v3/service/auth"
	"github.com/micro/micro/v3/service/store"
	"github.com/stretchr/testify/require"
)

type finalChunkReader struct {
	data     []byte
	terminal error
	closed   bool
}

func (r *finalChunkReader) Read(p []byte) (int, error) {
	n := copy(p, r.data)
	r.data = r.data[n:]
	if len(r.data) == 0 {
		return n, r.terminal
	}
	return n, nil
}
func (r *finalChunkReader) Close() error { r.closed = true; return nil }

type readOnlyBlob struct {
	store.BlobStore
	reader io.Reader
}

func (b readOnlyBlob) Read(string, ...store.BlobOption) (io.Reader, error) { return b.reader, nil }

type collectingReadStream struct {
	pb.BlobStore_ReadStream
	data   []byte
	closed bool
}

func (s *collectingReadStream) Send(r *pb.BlobReadResponse) error {
	s.data = append(s.data, r.Blob...)
	return nil
}
func (s *collectingReadStream) Close() error { s.closed = true; return nil }

func TestBlobReadPreservesFinalChunk(t *testing.T) {
	original := store.DefaultBlobStore
	t.Cleanup(func() { store.DefaultBlobStore = original })
	for _, size := range []int{1, 1024, 3900} {
		data := bytes.Repeat([]byte("x"), size)
		reader := &finalChunkReader{data: data, terminal: io.EOF}
		store.DefaultBlobStore = readOnlyBlob{reader: reader}
		ctx := auth.ContextWithAccount(context.Background(), &auth.Account{Issuer: "micro", Type: "service", Scopes: []string{"service"}})
		stream := &collectingReadStream{}
		err := new(BlobStore).Read(ctx, &pb.BlobReadRequest{Key: "fixture", Options: &pb.BlobOptions{Namespace: "micro"}}, stream)
		require.NoError(t, err)
		require.Equal(t, data, stream.data)
		require.True(t, stream.closed)
		require.True(t, reader.closed)
	}
}
func TestBlobReadPropagatesReaderFailure(t *testing.T) {
	original := store.DefaultBlobStore
	t.Cleanup(func() { store.DefaultBlobStore = original })
	failure := errors.New("reader failure")
	reader := &finalChunkReader{data: []byte("partial"), terminal: failure}
	store.DefaultBlobStore = readOnlyBlob{reader: reader}
	ctx := auth.ContextWithAccount(context.Background(), &auth.Account{Issuer: "micro", Type: "service", Scopes: []string{"service"}})
	stream := &collectingReadStream{}
	require.ErrorIs(t, new(BlobStore).Read(ctx, &pb.BlobReadRequest{Key: "fixture", Options: &pb.BlobOptions{Namespace: "micro"}}, stream), failure)
	require.False(t, stream.closed)
	require.True(t, reader.closed)
}
