// Package cprpc was forked from the standard package net/rpc:
// https://golang.org/pkg/net/rpc/

// Original work Copyright 2009 The Go Authors
// Modified work Copyright 2019 Hmgle (dustgle@gmail.com)
// All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package cprpc

import (
	"bufio"
	"context"
	"encoding/gob"
	"errors"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

const (
	// Defaults used by HandleHTTP
	DefaultRPCPath   = "/_goRPC_"
	DefaultDebugPath = "/debug/rpc"
)

// Request is a header written before every RPC call. It is used internally
// but documented here as an aid to debugging, such as when analyzing
// network traffic.
type Request struct {
	Path string
	Seq  uint64   // sequence number chosen by client
	next *Request // for free list in Server
}

// Response is a header written before every RPC return. It is used internally
// but documented here as an aid to debugging, such as when analyzing
// network traffic.
type Response struct {
	Path  string
	Seq   uint64    // echoes that of the request
	Error string    // error, if any.
	next  *Response // for free list in Server
}

type rpcConn struct {
	numActiveReq int32
	rwc          io.ReadWriteCloser
}

func newRpcConn(c io.ReadWriteCloser) *rpcConn {
	return &rpcConn{
		rwc: c,
	}
}

func (rc *rpcConn) incrNumReq() {
	atomic.AddInt32(&rc.numActiveReq, 1)
}

func (rc *rpcConn) decrNumReq() {
	atomic.AddInt32(&rc.numActiveReq, -1)
}

func (rc *rpcConn) numReq() int32 {
	return atomic.LoadInt32(&rc.numActiveReq)
}

// ErrServerClosed is returned by the Server's Serve method
// after a call to Shutdown or Close.
var ErrServerClosed = errors.New("cprpc: Server closed")

// Server represents an RPC Server.
type Server struct {
	reqLock  sync.Mutex // protects freeReq
	freeReq  *Request
	respLock sync.Mutex // protects freeResp
	freeResp *Response
	apis     map[string]API

	mu         sync.Mutex
	listeners  map[net.Listener]struct{}
	activeConn map[*rpcConn]struct{}
	inShutdown int32 // accessed atomically (non-zero means we're in Shutdown)
}

// NewServer returns a new Server.
func NewServer() *Server {
	return &Server{
		apis: make(map[string]API),
	}
}

// DefaultServer is the default instance of *Server.
var DefaultServer = NewServer()

type Context struct {
	conn    *Server
	seq     uint64
	req     *Request
	codec   ServerCodec
	sending *sync.Mutex
}

type API interface {
	Serve(*Context)
}

func (server *Server) RegisterAPI(path string, api API) {
	server.apis[path] = api
}

// A value sent as a placeholder for the server's response value when the server
// receives an invalid request. It is never decoded by the client since the Response
// contains an error when it is used.
var invalidRequest = struct{}{}

func (server *Server) sendResponse(sending *sync.Mutex, req *Request, reply interface{}, codec ServerCodec, errmsg string) {
	resp := server.getResponse()
	// Encode the response header
	resp.Path = req.Path
	if errmsg != "" {
		resp.Error = errmsg
		reply = invalidRequest
	}
	resp.Seq = req.Seq
	sending.Lock()
	err := codec.WriteResponse(resp, reply)
	if err != nil {
		log.Println("rpc: writing response:", err)
	}
	sending.Unlock()
	server.freeResponse(resp)
}

func (ctx *Context) ReplyOk(ret interface{}) {
	ctx.conn.sendResponse(ctx.sending, ctx.req, ret, ctx.codec, "")
	ctx.conn.freeRequest(ctx.req)
}

type gobServerCodec struct {
	rwc    io.ReadWriteCloser
	dec    *gob.Decoder
	enc    *gob.Encoder
	encBuf *bufio.Writer
	closed bool
}

func (c *gobServerCodec) ReadRequestHeader(r *Request) error {
	return c.dec.Decode(r)
}

func (c *gobServerCodec) ReadRequestBody(body interface{}) error {
	return c.dec.Decode(body)
}

func (c *gobServerCodec) WriteResponse(r *Response, body interface{}) (err error) {
	if err = c.enc.Encode(r); err != nil {
		if c.encBuf.Flush() == nil {
			// Gob couldn't encode the header. Should not happen, so if it does,
			// shut down the connection to signal that the connection is broken.
			log.Println("rpc: gob error encoding response:", err)
			c.Close()
		}
		return
	}
	if err = c.enc.Encode(body); err != nil {
		if c.encBuf.Flush() == nil {
			// Was a gob problem encoding the body but the header has been written.
			// Shut down the connection to signal that the connection is broken.
			log.Println("rpc: gob error encoding body:", err)
			c.Close()
		}
		return
	}
	return c.encBuf.Flush()
}

func (c *gobServerCodec) Close() error {
	if c.closed {
		// Only call c.rwc.Close once; otherwise the semantics are undefined.
		return nil
	}
	c.closed = true
	return c.rwc.Close()
}

func (c *gobServerCodec) GetRwc() io.ReadWriteCloser {
	return c.rwc
}

// ServeConn runs the server on a single connection.
// ServeConn blocks, serving the connection until the client hangs up.
// The caller typically invokes ServeConn in a go statement.
// ServeConn uses the gob wire format (see package gob) on the
// connection. To use an alternate codec, use ServeCodec.
// See NewClient's comment for information about concurrent access.
func (server *Server) ServeConn(conn io.ReadWriteCloser) {
	buf := bufio.NewWriter(conn)
	srv := &gobServerCodec{
		rwc:    conn,
		dec:    gob.NewDecoder(conn),
		enc:    gob.NewEncoder(buf),
		encBuf: buf,
	}
	server.ServeCodec(srv)
}

// ServeCodec is like ServeConn but uses the specified codec to
// decode requests and encode responses.
func (server *Server) ServeCodec(codec ServerCodec) {
	rc := newRpcConn(codec.GetRwc())
	server.trackConn(rc, true)

	sending := new(sync.Mutex)
	wg := new(sync.WaitGroup)
	for {
		api, req, keepReading, err := server.readRequest(codec, rc)
		if err != nil {
			if !keepReading {
				break
			}
			// send a response if we actually managed to read a header.
			if req != nil {
				server.sendResponse(sending, req, invalidRequest, codec, err.Error())
				server.freeRequest(req)
			}
			rc.decrNumReq()
			continue
		}
		wg.Add(1)
		go func() {
			api.Serve(&Context{
				conn:    server,
				seq:     req.Seq,
				req:     req,
				codec:   codec,
				sending: sending,
			})
			rc.decrNumReq()
			wg.Done()
		}()
	}
	// We've seen that there are no more requests.
	// Wait for responses to be sent before closing codec.
	wg.Wait()
	codec.Close()
	server.trackConn(rc, false)
}

// ServeRequest is like ServeCodec but synchronously serves a single request.
// It does not close the codec upon completion.
func (server *Server) ServeRequest(codec ServerCodec) error {
	rc := newRpcConn(codec.GetRwc())
	server.trackConn(rc, true)

	sending := new(sync.Mutex)
	api, req, keepReading, err := server.readRequest(codec, rc)
	if err != nil {
		defer rc.decrNumReq()
		if !keepReading {
			return err
		}
		// send a response if we actually managed to read a header.
		if req != nil {
			server.sendResponse(sending, req, invalidRequest, codec, err.Error())
			server.freeRequest(req)
		}
		return err
	}
	api.Serve(&Context{
		conn:    server,
		seq:     req.Seq,
		req:     req,
		codec:   codec,
		sending: sending,
	})
	rc.decrNumReq()
	return nil
}

func (server *Server) getRequest() *Request {
	server.reqLock.Lock()
	req := server.freeReq
	if req == nil {
		req = new(Request)
	} else {
		server.freeReq = req.next
		*req = Request{}
	}
	server.reqLock.Unlock()
	return req
}

func (server *Server) freeRequest(req *Request) {
	server.reqLock.Lock()
	req.next = server.freeReq
	server.freeReq = req
	server.reqLock.Unlock()
}

func (server *Server) getResponse() *Response {
	server.respLock.Lock()
	resp := server.freeResp
	if resp == nil {
		resp = new(Response)
	} else {
		server.freeResp = resp.next
		*resp = Response{}
	}
	server.respLock.Unlock()
	return resp
}

func (server *Server) freeResponse(resp *Response) {
	server.respLock.Lock()
	resp.next = server.freeResp
	server.freeResp = resp
	server.respLock.Unlock()
}

func (server *Server) readRequest(codec ServerCodec, rc *rpcConn) (api API, req *Request, keepReading bool, err error) {
	api, req, keepReading, err = server.readRequestHeader(codec)
	rc.incrNumReq()
	if err != nil {
		if !keepReading {
			return
		}
		// discard body
		codec.ReadRequestBody(nil)
		return
	}

	if err = codec.ReadRequestBody(api); err != nil {
		log.Panic(err)
	}
	return
}

func (server *Server) readRequestHeader(codec ServerCodec) (api API, req *Request, keepReading bool, err error) {
	// Grab the request header.
	req = server.getRequest()
	err = codec.ReadRequestHeader(req)
	if err != nil {
		req = nil
		if err == io.EOF || err == io.ErrUnexpectedEOF {
			return
		}
		err = errors.New("rpc: server cannot decode request: " + err.Error())
		return
	}

	// We read the header successfully. If we see an error now,
	// we can still recover and move on to the next request.
	keepReading = true

	api, ok := server.apis[req.Path]
	if !ok {
		err = errors.New("rpc: can't find path " + req.Path)
	}
	return
}

func (server *Server) serve(lis net.Listener) error {
	defer lis.Close()
	if !server.trackListener(lis, true) {
		return ErrServerClosed
	}
	defer server.trackListener(lis, false)

	for {
		conn, err := lis.Accept()
		if err != nil {
			if server.shuttingDown() {
				atomic.StoreInt32(&server.inShutdown, 0)
				return ErrServerClosed
			}
			return err
		}
		go server.ServeConn(conn)
	}
}

// Serve accepts connections on the listener and serves requests
// for each incoming connection. Accept blocks until the listener
// returns a non-nil error. The caller typically invokes Accept in a
// go statement.
func (server *Server) Serve(lis net.Listener) error {
	go server.serve(lis)

	sigCh := make(chan os.Signal, 10)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP, syscall.SIGUSR2)
	for {
		sig := <-sigCh
		switch sig {
		case syscall.SIGINT, syscall.SIGTERM:
			return server.Shutdown(context.Background())
		case syscall.SIGHUP, syscall.SIGUSR2:
			p, err := forkChild()
			if err != nil {
				log.Printf("unable to fork child: %v.\n", err)
				continue
			}
			log.Printf("forked child %v.\n", p.Pid)
			return server.Shutdown(context.Background())
		}
	}
}

var allProcFiles = []*os.File{os.Stdin, os.Stdout, os.Stderr}

// startProcess starts a new process passing it the active listeners. It
// doesn't fork, but starts a new process using the same environment and
// arguments as when it was originally started. This allows for a newly
// deployed binary to be started. It returns the pid of the newly started
// process when successful.
func startProcess() (int, error) {
	execName, err := os.Executable()
	if err != nil {
		return 0, err
	}
	execDir := filepath.Dir(execName)

	for _, f := range allProcFiles {
		defer f.Close()
	}

	// Use the original binary location. This works with symlinks such that if
	// the file it points to has been changed we will use the updated symlink.
	argv0, err := exec.LookPath(os.Args[0])
	if err != nil {
		return 0, err
	}

	process, err := os.StartProcess(argv0, os.Args, &os.ProcAttr{
		Dir:   execDir,
		Env:   os.Environ(),
		Files: allProcFiles,
		Sys:   &syscall.SysProcAttr{},
	})
	if err != nil {
		return 0, err
	}
	return process.Pid, nil
}

func forkChild() (*os.Process, error) {
	execName, err := os.Executable()
	if err != nil {
		return nil, err
	}
	execDir := filepath.Dir(execName)
	// Spawn child process.
	p, err := os.StartProcess(execName, []string{execName}, &os.ProcAttr{
		Dir: execDir,
		Sys: &syscall.SysProcAttr{},
	})
	if err != nil {
		return nil, err
	}

	return p, nil
}

// shutdownPollInterval is how often we poll for quiescence
// during Server.Shutdown. This is lower during tests, to
// speed up tests.
var shutdownPollInterval = 500 * time.Millisecond

// Shutdown gracefully shuts down the server without interrupting any
// active connections. Shutdown works by first closing all open
// listeners, then closing all idle connections, and then waiting
// indefinitely for connections to return to idle and then shut down.
// If the provided context expires before the shutdown is complete,
// Shutdown returns the context's error, otherwise it returns any
// error returned from closing the Server's underlying Listener(s).
func (server *Server) Shutdown(ctx context.Context) error {
	atomic.StoreInt32(&server.inShutdown, 1)

	server.mu.Lock()
	lnerr := server.closeListenersLocked()
	server.mu.Unlock()

	ticker := time.NewTicker(shutdownPollInterval)
	defer ticker.Stop()
	for {
		if server.closeIdleConns() {
			return lnerr
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

// Close immediately closes all active net.Listeners and any
// connections in state StateNew, StateActive, or StateIdle. For a
// graceful shutdown, use Shutdown.
func (server *Server) Close() error {
	atomic.StoreInt32(&server.inShutdown, 1)
	server.mu.Lock()
	defer server.mu.Unlock()
	err := server.closeListenersLocked()
	for rc := range server.activeConn {
		rc.rwc.Close()
		delete(server.activeConn, rc)
	}
	return err
}

func (s *Server) closeListenersLocked() error {
	var err error
	for ln := range s.listeners {
		if cerr := ln.Close(); err != nil {
			err = cerr
		}
		delete(s.listeners, ln)
	}
	return err
}

func (s *Server) trackConnUnlock(rc *rpcConn, add bool) {
	if s.activeConn == nil {
		s.activeConn = make(map[*rpcConn]struct{})
	}
	if add {
		s.activeConn[rc] = struct{}{}
	} else {
		delete(s.activeConn, rc)
	}
}

func (s *Server) trackConn(rc *rpcConn, add bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.activeConn == nil {
		s.activeConn = make(map[*rpcConn]struct{})
	}
	if add {
		s.activeConn[rc] = struct{}{}
	} else {
		delete(s.activeConn, rc)
	}
}

func (s *Server) shuttingDown() bool {
	return atomic.LoadInt32(&s.inShutdown) != 0
}

func (s *Server) trackListener(lis net.Listener, add bool) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.listeners == nil {
		s.listeners = make(map[net.Listener]struct{})
	}
	if add {
		if s.shuttingDown() {
			return false
		}
		s.listeners[lis] = struct{}{}
	} else {
		delete(s.listeners, lis)
	}
	return true
}

// closeIdleConns closes all idle connections and reports whether the
// server is quiescent.
func (s *Server) closeIdleConns() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	quiescent := true
	for rc := range s.activeConn {
		if rc.numReq() == 0 {
			rc.rwc.Close()
			s.trackConnUnlock(rc, false)
		} else {
			quiescent = false
		}
	}
	return quiescent
}

// A ServerCodec implements reading of RPC requests and writing of
// RPC responses for the server side of an RPC session.
// The server calls ReadRequestHeader and ReadRequestBody in pairs
// to read requests from the connection, and it calls WriteResponse to
// write a response back. The server calls Close when finished with the
// connection. ReadRequestBody may be called with a nil
// argument to force the body of the request to be read and discarded.
// See NewClient's comment for information about concurrent access.
type ServerCodec interface {
	ReadRequestHeader(*Request) error
	ReadRequestBody(interface{}) error
	WriteResponse(*Response, interface{}) error

	// Close can be called multiple times and must be idempotent.
	Close() error

	GetRwc() io.ReadWriteCloser
}

// ServeConn runs the DefaultServer on a single connection.
// ServeConn blocks, serving the connection until the client hangs up.
// The caller typically invokes ServeConn in a go statement.
// ServeConn uses the gob wire format (see package gob) on the
// connection. To use an alternate codec, use ServeCodec.
// See NewClient's comment for information about concurrent access.
func ServeConn(conn io.ReadWriteCloser) {
	DefaultServer.ServeConn(conn)
}

// ServeCodec is like ServeConn but uses the specified codec to
// decode requests and encode responses.
func ServeCodec(codec ServerCodec) {
	DefaultServer.ServeCodec(codec)
}

// ServeRequest is like ServeCodec but synchronously serves a single request.
// It does not close the codec upon completion.
func ServeRequest(codec ServerCodec) error {
	return DefaultServer.ServeRequest(codec)
}

// Serve accepts connections on the listener and serves requests
// to DefaultServer for each incoming connection.
// Accept blocks; the caller typically invokes it in a go statement.
func Serve(lis net.Listener) error {
	return DefaultServer.Serve(lis)
}

// Can connect to RPC service using HTTP CONNECT to rpcPath.
var connected = "200 Connected to Go RPC"

// ServeHTTP implements an http.Handler that answers RPC requests.
func (server *Server) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	if req.Method != "CONNECT" {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusMethodNotAllowed)
		io.WriteString(w, "405 must CONNECT\n")
		return
	}
	conn, _, err := w.(http.Hijacker).Hijack()
	if err != nil {
		log.Print("rpc hijacking ", req.RemoteAddr, ": ", err.Error())
		return
	}
	io.WriteString(conn, "HTTP/1.0 "+connected+"\n\n")
	server.ServeConn(conn)
}

// HandleHTTP registers an HTTP handler for RPC messages on rpcPath,
// and a debugging handler on debugPath.
// It is still necessary to invoke http.Serve(), typically in a go statement.
func (server *Server) HandleHTTP(rpcPath, debugPath string) {
	http.Handle(rpcPath, server)
}

// HandleHTTP registers an HTTP handler for RPC messages to DefaultServer
// on DefaultRPCPath and a debugging handler on DefaultDebugPath.
// It is still necessary to invoke http.Serve(), typically in a go statement.
func HandleHTTP() {
	DefaultServer.HandleHTTP(DefaultRPCPath, DefaultDebugPath)
}

// Shutdown gracefully shuts down the DefaultServer without interrupting any
// active connection. Shutdown works by first closing all DefaultServer's
// open listeners, then waiting indefinitely for connections to transition to
// an idle state before closing them and shutting down. If the provided context
// expires before the shutdown is complete, Shutdown forcibly closes active
// connections and returns the context's error, otherwise it returns any error
// returned from closing the DefaultServer's underlying Listener(s).
func Shutdown(ctx context.Context) error {
	return DefaultServer.Shutdown(ctx)
}
