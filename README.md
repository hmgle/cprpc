# cprpc

`cprpc` is a RPC package that copied and modified from the standard package of `net/rpc`, provides an easier way to use:

## Usage

Server:

```go
package main

import (
	"log"
	"net"

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

func main() {
	srv := cprpc.NewServer()
	srv.RegisterAPI("/v1/hello", &HelloV1API{})

	listener, err := net.Listen("tcp", ":1234")
	if err != nil {
		log.Fatal("ListenTCP error:", err)
	}
	srv.Serve(listener)
}
```

Client:

```go
package main

import (
	"log"

	"github.com/hmgle/cprpc"
)

func main() {
	client, err := cprpc.Dial("tcp4", "127.0.0.1:1234")
	if err != nil {
		log.Fatal(err)
	}
	args := &HelloV1API{
		Name: "world",
	}
	reply := &HelloV1Ret{}
	err = client.Call("/v1/hello", args, &reply)
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("reply: %+v\n", reply)
}
```

## Performance

Below are benchmark results comparing cprpc performance to net/rpc:

```
$ go test -bench=. -benchmem -run=none
goos: linux
goarch: amd64
pkg: github.com/hmgle/cprpc
cpu: Intel(R) Core(TM) i5-5200U CPU @ 2.20GHz
BenchmarkCprpc-4                   23352             51321 ns/op             328 B/op         11 allocs/op
BenchmarkCprpcJSON-4               17560             64298 ns/op             336 B/op         13 allocs/op
BenchmarkCprpcMsgpack-4            13821             83964 ns/op             328 B/op         11 allocs/op
BenchmarkCprpcPool-4               15102             74708 ns/op            1070 B/op         22 allocs/op
BenchmarkCprpcPoolJSON-4           19048             62061 ns/op             368 B/op         13 allocs/op
BenchmarkCprpcPoolMsgpack-4        12912             86324 ns/op             360 B/op         12 allocs/op
BenchmarkNetRpc-4                  18945             62589 ns/op             358 B/op         12 allocs/op
PASS
ok      github.com/hmgle/cprpc  13.357s
```
