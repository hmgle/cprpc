package jsonrpc

import (
	"bufio"
	"encoding/json"
	"io"

	"github.com/hmgle/cprpc"
)

type clientCodec struct {
	rwc    io.ReadWriteCloser
	dec    *json.Decoder // for reading JSON values
	enc    *json.Encoder // for writing JSON values
	encBuf *bufio.Writer
}

var _ cprpc.ClientCodec = &clientCodec{}

// NewClientCodec returns a new cprpc.ClientCodec using JSON-RPC on conn.
func NewClientCodec(conn io.ReadWriteCloser) cprpc.ClientCodec {
	return &clientCodec{
		dec:    json.NewDecoder(conn),
		enc:    json.NewEncoder(conn),
		rwc:    conn,
		encBuf: bufio.NewWriter(conn),
	}
}

func (c *clientCodec) WriteRequest(r *cprpc.Request, body interface{}) (err error) {
	if err = c.enc.Encode(r); err != nil {
		return
	}
	if err = c.enc.Encode(body); err != nil {
		return
	}
	return c.encBuf.Flush()
}

func (c *clientCodec) ReadResponseHeader(r *cprpc.Response) error {
	return c.dec.Decode(r)
}

func (c *clientCodec) ReadResponseBody(body interface{}) error {
	return c.dec.Decode(body)
}

func (c *clientCodec) Close() error {
	return c.rwc.Close()
}
