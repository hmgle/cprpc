# cprpc

`cprpc` is a RPC package that copied and modified from the standard package of `net/rpc`, provides an easier way to use:

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
	srv.Accept(listener)
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
