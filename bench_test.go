package cprpc_test

import (
	"log"
	"net"
	"testing"

	"github.com/hmgle/cprpc"
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
	go srv.Accept(l)

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
