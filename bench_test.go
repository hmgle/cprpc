package cprpc_test

import (
	"log"
	"net"
	"net/rpc"
	"testing"
	"time"

	"github.com/hmgle/cprpc"
	"github.com/hmgle/cprpc/codec/jsonrpc"
	"github.com/hmgle/cprpc/codec/msgpackrpc"
)

type (
	HelloV1API struct {
		Name string
	}
	HelloV1Ret struct {
		Data string
	}
)

func (h *HelloV1API) Serve(ctx *cprpc.Context) {
	ctx.ReplyOk(&HelloV1Ret{
		Data: "hello, " + h.Name,
	})
}

func BenchmarkCprpc(b *testing.B) {
	b.StopTimer()
	srv := cprpc.NewServer()
	srv.RegisterAPI("/v1/hello", &HelloV1API{})

	l, err := net.Listen("tcp4", "")
	if err != nil {
		log.Fatal(err)
	}
	go srv.Serve(l)

	client, err := cprpc.Dial("tcp4", l.Addr().String())
	if err != nil {
		log.Fatal(err)
	}
	defer client.Close()
	b.StartTimer()
	args := &HelloV1API{
		Name: "world",
	}
	reply := &HelloV1Ret{}
	for i := 0; i < b.N; i++ {
		client.Call("/v1/hello", &args, reply)
	}
	b.StopTimer()
}

func BenchmarkCprpcJSON(b *testing.B) {
	b.StopTimer()
	srv := cprpc.NewServer(jsonrpc.NewServerCodec)
	srv.RegisterAPI("/v1/hello", &HelloV1API{})

	l, err := net.Listen("tcp4", "")
	if err != nil {
		log.Fatal(err)
	}
	go srv.Serve(l)

	client, err := cprpc.Dial("tcp4", l.Addr().String(), jsonrpc.NewClientCodec)
	if err != nil {
		log.Fatal(err)
	}
	defer client.Close()
	b.StartTimer()
	args := &HelloV1API{
		Name: "world",
	}
	reply := &HelloV1Ret{}
	for i := 0; i < b.N; i++ {
		client.Call("/v1/hello", &args, reply)
	}
	b.StopTimer()
}

func BenchmarkCprpcMsgpack(b *testing.B) {
	b.StopTimer()
	srv := cprpc.NewServer(msgpackrpc.NewServerCodec)
	srv.RegisterAPI("/v1/hello", &HelloV1API{})

	l, err := net.Listen("tcp4", "")
	if err != nil {
		log.Fatal(err)
	}
	go srv.Serve(l)

	client, err := cprpc.Dial("tcp4", l.Addr().String(), msgpackrpc.NewClientCodec)
	if err != nil {
		log.Fatal(err)
	}
	defer client.Close()
	b.StartTimer()
	args := &HelloV1API{
		Name: "world",
	}
	reply := &HelloV1Ret{}
	for i := 0; i < b.N; i++ {
		client.Call("/v1/hello", &args, reply)
	}
	b.StopTimer()
}

func BenchmarkCprpcPool(b *testing.B) {
	b.StopTimer()
	srv := cprpc.NewServer()
	srv.RegisterAPI("/v1/hello", &HelloV1API{})

	l, err := net.Listen("tcp4", "")
	if err != nil {
		log.Fatal(err)
	}
	go srv.Serve(l)

	options := &cprpc.Options{
		InitTargets:  []string{l.Addr().String()},
		InitCap:      5,
		MaxCap:       30,
		DialTimeout:  time.Second * 5,
		IdleTimeout:  time.Second * 60,
		ReadTimeout:  time.Second * 5,
		WriteTimeout: time.Second * 5,
	}

	clientPool, err := cprpc.NewRPCPool(options)
	if err != nil {
		log.Fatal(err)
	}

	b.StartTimer()
	args := &HelloV1API{
		Name: "world",
	}
	reply := &HelloV1Ret{}
	for i := 0; i < b.N; i++ {
		err = clientPool.Call("/v1/hello", &args, reply)
		if err != nil {
			log.Fatal(err)
		}
	}
	b.StopTimer()
}

func BenchmarkCprpcPoolJSON(b *testing.B) {
	b.StopTimer()
	srv := cprpc.NewServer(jsonrpc.NewServerCodec)
	srv.RegisterAPI("/v1/hello", &HelloV1API{})

	l, err := net.Listen("tcp4", "")
	if err != nil {
		log.Fatal(err)
	}
	go srv.Serve(l)

	options := &cprpc.Options{
		InitTargets:  []string{l.Addr().String()},
		InitCap:      5,
		MaxCap:       30,
		DialTimeout:  time.Second * 5,
		IdleTimeout:  time.Second * 60,
		ReadTimeout:  time.Second * 5,
		WriteTimeout: time.Second * 5,
		CodecFunc:    jsonrpc.NewClientCodec,
	}

	clientPool, err := cprpc.NewRPCPool(options)
	if err != nil {
		log.Fatal(err)
	}

	b.StartTimer()
	args := &HelloV1API{
		Name: "world",
	}
	reply := &HelloV1Ret{}
	for i := 0; i < b.N; i++ {
		err = clientPool.Call("/v1/hello", &args, reply)
		if err != nil {
			log.Fatal(err)
		}
	}
	b.StopTimer()
}

func BenchmarkCprpcPoolMsgpack(b *testing.B) {
	b.StopTimer()
	srv := cprpc.NewServer(msgpackrpc.NewServerCodec)
	srv.RegisterAPI("/v1/hello", &HelloV1API{})

	l, err := net.Listen("tcp4", "")
	if err != nil {
		log.Fatal(err)
	}
	go srv.Serve(l)

	options := &cprpc.Options{
		InitTargets:  []string{l.Addr().String()},
		InitCap:      5,
		MaxCap:       30,
		DialTimeout:  time.Second * 5,
		IdleTimeout:  time.Second * 60,
		ReadTimeout:  time.Second * 5,
		WriteTimeout: time.Second * 5,
		CodecFunc:    msgpackrpc.NewClientCodec,
	}

	clientPool, err := cprpc.NewRPCPool(options)
	if err != nil {
		log.Fatal(err)
	}

	b.StartTimer()
	args := &HelloV1API{
		Name: "world",
	}
	reply := &HelloV1Ret{}
	for i := 0; i < b.N; i++ {
		err = clientPool.Call("/v1/hello", &args, reply)
		if err != nil {
			log.Fatal(err)
		}
	}
	b.StopTimer()
}

type (
	HelloSvr  int
	HelloArgs struct {
		Name string
	}
	HelloRet struct {
		Data string
	}
)

func (h *HelloSvr) Hello(args *HelloArgs, reply *HelloRet) error {
	reply.Data = "hello, " + args.Name
	return nil
}

func BenchmarkNetRpc(b *testing.B) {
	b.StopTimer()
	srv := new(HelloSvr)
	rpc.Register(srv)

	l, err := net.Listen("tcp4", "")
	if err != nil {
		log.Fatal(err)
	}
	go rpc.Accept(l)

	client, err := rpc.Dial("tcp", l.Addr().String())
	if err != nil {
		log.Fatal(err)
	}

	args := &HelloArgs{
		Name: "world",
	}
	reply := new(HelloRet)

	b.StartTimer()
	for i := 0; i < b.N; i++ {
		err = client.Call("HelloSvr.Hello", args, reply)
		if err != nil {
			log.Fatal(err)
		}
	}
	b.StopTimer()
}
