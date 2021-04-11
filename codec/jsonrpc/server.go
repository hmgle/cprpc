package jsonrpc

import (
	"bufio"
	"encoding/json"
	"io"
	"log"

	"github.com/hmgle/cprpc"
)

var _ cprpc.ServerCodec = &serverCodec{}

type serverCodec struct {
	rwc    io.ReadWriteCloser
	dec    *json.Decoder // for reading JSON values
	enc    *json.Encoder // for writing JSON values
	encBuf *bufio.Writer
	closed bool
}

// NewServerCodec returns a new cprpc.ServerCodec using JSON-RPC on conn.
func NewServerCodec(conn io.ReadWriteCloser) cprpc.ServerCodec {
	return &serverCodec{
		rwc:    conn,
		dec:    json.NewDecoder(conn),
		enc:    json.NewEncoder(conn),
		encBuf: bufio.NewWriter(conn),
	}
}

func (c *serverCodec) ReadRequestHeader(r *cprpc.Request) error {
	return c.dec.Decode(r)
}

func (c *serverCodec) WriteResponse(r *cprpc.Response, body interface{}) (err error) {
	if err = c.enc.Encode(r); err != nil {
		if c.encBuf.Flush() == nil {
			// couldn't encode the header. Should not happen, so if it does,
			// shut down the connection to signal that the connection is broken.
			log.Println("rpc: error encoding response:", err)
			c.Close()
		}
		return
	}
	if err = c.enc.Encode(body); err != nil {
		if c.encBuf.Flush() == nil {
			// Was a problem encoding the body but the header has been written.
			// Shut down the connection to signal that the connection is broken.
			log.Println("rpc: error encoding body:", err)
			c.Close()
		}
		return
	}
	return c.encBuf.Flush()
}

func (c *serverCodec) ReadRequestBody(body interface{}) error {
	return c.dec.Decode(body)
}

func (c *serverCodec) Close() error {
	if c.closed {
		// Only call c.rwc.Close once; otherwise the semantics are undefined.
		return nil
	}
	c.closed = true
	return c.rwc.Close()
}

func (c *serverCodec) GetRwc() io.ReadWriteCloser {
	return c.rwc
}
