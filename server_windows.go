package cprpc

import (
	"net"
)

// Serve accepts connections on the listener and serves requests
// for each incoming connection. Accept blocks until the listener
// returns a non-nil error. The caller typically invokes Accept in a
// go statement.
func (server *Server) Serve(lis net.Listener) error {
	server.serve(lis)
}
