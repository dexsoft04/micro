package client

import (
	"bytes"
	"context"
	pb "github.com/micro/micro/v3/proto/store"
	microclient "github.com/micro/micro/v3/service/client"
	"github.com/micro/micro/v3/service/store"
	"github.com/stretchr/testify/require"
	"io"
	"testing"
)

type eofReader struct{ data []byte }

func (r *eofReader) Read(p []byte) (int, error) {
	n := copy(p, r.data)
	r.data = r.data[n:]
	if len(r.data) == 0 {
		return n, io.EOF
	}
	return n, nil
}

type writeStream struct {
	pb.BlobStore_WriteService
	data   []byte
	closed bool
}

func (s *writeStream) Send(r *pb.BlobWriteRequest) error {
	s.data = append(s.data, r.Blob...)
	return nil
}
func (s *writeStream) CloseAndRecv() (*pb.BlobWriteResponse, error) {
	s.closed = true
	return &pb.BlobWriteResponse{}, nil
}

type blobService struct {
	pb.BlobStoreService
	stream *writeStream
}

func (s blobService) Write(context.Context, ...microclient.CallOption) (pb.BlobStore_WriteService, error) {
	return s.stream, nil
}
func TestBlobWritePreservesFinalChunk(t *testing.T) {
	for _, size := range []int{1, 1024, 3900} {
		data := bytes.Repeat([]byte("x"), size)
		stream := &writeStream{}
		b := &blob{client: blobService{stream: stream}}
		require.NoError(t, b.Write("fixture", &eofReader{data: data}, store.BlobNamespace("micro")))
		require.Equal(t, data, stream.data)
		require.True(t, stream.closed)
	}
}
