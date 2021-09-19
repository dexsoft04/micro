package websocket

import (
	"bytes"
	"github.com/micro/micro/v3/service/network/transport"
	"github.com/micro/micro/v3/util/codec"
	raw "github.com/micro/micro/v3/util/codec/bytes"
	"github.com/oxtoacart/bpool"
	"github.com/pkg/errors"
	"sync"
)
var (
	bufferPool = bpool.NewSizedBufferPool(32, 1)
)

func getHeader(hdr string, md map[string]string) string {
	if hd := md[hdr]; len(hd) > 0 {
		return hd
	}
	return md["X-"+hdr]
}

func setHeaders(m, r *codec.Message) {
	set := func(hdr, v string) {
		if len(v) == 0 {
			return
		}
		m.Header[hdr] = v
		m.Header["X-"+hdr] = v
	}

	// set headers
	set("Micro-Id", r.Id)
	set("Micro-Service", r.Target)
	set("Micro-Method", r.Method)
	set("Micro-Endpoint", r.Endpoint)
	set("Micro-Error", r.Error)
}

func getHeaders(m *codec.Message) {
	set := func(v, hdr string) string {
		if len(v) > 0 {
			return v
		}
		return m.Header[hdr]
	}

	m.Id = set(m.Id, "Micro-Id")
	m.Error = set(m.Error, "Micro-Error")
	m.Endpoint = set(m.Endpoint, "Micro-Endpoint")
	m.Method = set(m.Method, "Micro-Method")
	m.Target = set(m.Target, "Micro-Service")

	// TODO: remove this cruft
	if len(m.Endpoint) == 0 {
		m.Endpoint = m.Method
	}
}

type readWriteCloser struct {
	sync.RWMutex
	wbuf *bytes.Buffer
	rbuf *bytes.Buffer
}

func (rwc *readWriteCloser) Read(p []byte) (n int, err error) {
	rwc.RLock()
	defer rwc.RUnlock()
	return rwc.rbuf.Read(p)
}

func (rwc *readWriteCloser) Write(p []byte) (n int, err error) {
	rwc.Lock()
	defer rwc.Unlock()
	return rwc.wbuf.Write(p)
}

func (rwc *readWriteCloser) Close() error {
	return nil
}

type wsCodec struct {
	socket transport.Socket
	codec codec.Codec
	req *transport.Message
	buf *readWriteCloser
	
}

func (w *wsCodec) ReadHeader(r *codec.Message, messageType codec.MessageType) error {
	// the initial message
	m := codec.Message{
		Header: w.req.Header,
		Body:   w.req.Body,
	}
	// set some internal things
	getHeaders(&m)

	// read header via codec
	if err := w.codec.ReadHeader(&m, codec.Request); err != nil {
		return err
	}

	// fallback for 0.14 and older
	if len(m.Endpoint) == 0 {
		m.Endpoint = m.Method
	}

	// set message
	*r = m

	return nil
}

func (w *wsCodec) ReadBody(b interface{}) error {
	// don't read empty body
	if len(w.req.Body) == 0 {
		return nil
	}
	// read raw data
	if v, ok := b.(*raw.Frame); ok {
		v.Data = w.req.Body
		return nil
	}
	// decode the usual way
	return w.codec.ReadBody(b)
}

func (w *wsCodec) Write(r *codec.Message, b interface{}) error {
	w.buf.wbuf.Reset()

	// create a new message
	m := &codec.Message{
		Target:   r.Target,
		Method:   r.Method,
		Endpoint: r.Endpoint,
		Id:       r.Id,
		Error:    r.Error,
		Type:     r.Type,
		Header:   r.Header,
	}

	if m.Header == nil {
		m.Header = map[string]string{}
	}

	setHeaders(m, r)

	// the body being sent
	var body []byte

	// is it a raw frame?
	if v, ok := b.(*raw.Frame); ok {
		body = v.Data
		// if we have encoded data just send it
	} else if len(r.Body) > 0 {
		body = r.Body
		// write the body to codec
	} else if err := w.codec.Write(m, b); err != nil {
		w.buf.wbuf.Reset()

		// write an error if it failed
		m.Error = errors.Wrapf(err, "Unable to encode body").Error()
		m.Header["Micro-Error"] = m.Error
		// no body to write
		if err := w.codec.Write(m, nil); err != nil {
			return err
		}
	} else {
		// set the body
		body = w.buf.wbuf.Bytes()
	}

	// Set content type if theres content
	if len(body) > 0 {
		m.Header["Content-Type"] = w.req.Header["Content-Type"]
	}

	// send on the socket
	return w.socket.Send(&transport.Message{
		Header: m.Header,
		Body:   body,
	})
}

func (w *wsCodec) Close() error {
	// close the codec
	w.codec.Close()
	// close the socket
	err := w.socket.Close()
	// put back the buffers
	bufferPool.Put(w.buf.rbuf)
	bufferPool.Put(w.buf.wbuf)
	// return the error
	return err
}

func (w *wsCodec) String() string {
	return "mucp"
}

func newWsCodec(req *transport.Message, socket transport.Socket, c codec.NewCodec) codec.Codec {
	rwc := &readWriteCloser{
		rbuf: bufferPool.Get(),
		wbuf: bufferPool.Get(),
	}
	rwc.rbuf.Write(req.Body)
	return &wsCodec{
		socket: socket,
		codec:  c(rwc),
		req:    req,
		buf:    rwc,
	}
}