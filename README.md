# socket.io

## Features

**This project fork from** [socket.io](https://github.com/zishang520/socket.io)  
Socket.IO enables real-time bidirectional event-based communication. It consists of:

- **Support Socket.IO v4+ 🚀🚀🚀**
- a Golang server (this repository)
- a [Javascript client library](https://github.com/socketio/socket.io-client) for the browser (or a Node.js client)

#### Simple and convenient API

Sample code:

```golang
import (
    "github.com/zishang520/socket.io/v2/socket"
)
io.On("connection", func(clients ...any) {
    client := clients[0].(*socket.Socket)
    client.Emit("request" /* … */)                       // emit an event to the socket
    io.Emit("broadcast" /* … */)                         // emit an event to all connected sockets
    client.On("reply", func(...any) { /* … */ }) // listen to the event
})
```

#### Multiplexing support

In order to create separation of concerns within your application (for example per module, or based on permissions), Socket.IO allows you to create several `Namespaces`, which will act as separate communication channels but will share the same underlying connection.

#### Room support

Within each `Namespace`, you can define arbitrary channels, called `Rooms`, that sockets can join and leave. You can then broadcast to any given room, reaching every socket that has joined it.

This is a useful feature to send notifications to a group of users, or to a given user connected on several devices for example.

**Note:** Socket.IO is not a WebSocket implementation. Although Socket.IO indeed uses WebSocket as a transport when possible, it adds some metadata to each packet: the packet type, the namespace and the ack id when a message acknowledgement is needed. That is why a WebSocket client will not be able to successfully connect to a Socket.IO server, and a Socket.IO client will not be able to connect to a WebSocket server (like `ws://echo.websocket.org`) either. Please see the protocol specification [here](https://github.com/socketio/socket.io-protocol).

## How to use

The following example attaches socket.io to a plain engine.io \*types.CreateServer listening on port `3000`.

```golang
package main

import (
    "github.com/zishang520/engine.io/types"
    "github.com/zishang520/engine.io/utils"
    "github.com/zishang520/socket.io/v2/socket"
    "os"
    "os/signal"
    "syscall"
)

func main() {
    httpServer := types.CreateServer(nil)
    io := socket.NewServer(httpServer, nil)
    io.On("connection", func(clients ...any) {
        client := clients[0].(*socket.Socket)
        client.On("event", func(datas ...any) {
        })
        client.On("disconnect", func(...any) {
        })
    })
    httpServer.Listen("127.0.0.1:3000", nil)

    exit := make(chan struct{})
    SignalC := make(chan os.Signal)

    signal.Notify(SignalC, os.Interrupt, syscall.SIGHUP, syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT)
    go func() {
        for s := range SignalC {
            switch s {
            case os.Interrupt, syscall.SIGHUP, syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT:
                close(exit)
                return
            }
        }
    }()

    <-exit
    httpServer.Close(nil)
    os.Exit(0)
}
```

other: Use [http.Handler](https://pkg.go.dev/net/http#Handler) interface

```golang
package main

import (
    "net/http"
    "os"
    "os/signal"
    "syscall"

    "github.com/zishang520/socket.io/v2/socket"
)

func main() {
    io := socket.NewServer(nil, nil)
    http.Handle("/socket.io/", io.ServeHandler(nil))
    go http.ListenAndServe(":3000", nil)

    io.On("connection", func(clients ...any) {
        client := clients[0].(*socket.Socket)
        client.On("event", func(datas ...any) {
        })
        client.On("disconnect", func(...any) {
        })
    })

    exit := make(chan struct{})
    SignalC := make(chan os.Signal)

    signal.Notify(SignalC, os.Interrupt, syscall.SIGHUP, syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT)
    go func() {
        for s := range SignalC {
            switch s {
            case os.Interrupt, syscall.SIGHUP, syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT:
                close(exit)
                return
            }
        }
    }()

    <-exit
    io.Close(nil)
    os.Exit(0)
}

```

Gin Route: Use [http.Handler](https://pkg.go.dev/net/http#Handler) interface

```golang
import (
	"log"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/zishang520/socket.io/socket" // 支持 4 以上的其他版本
)

func cross(ctx *gin.Context) {
	ctx.Header("Access-Control-Allow-Origin", "http://localhost:3000")
	ctx.Header("Access-Control-Allow-Headers", "Content-Type,AccessToken,X-CSRF-Token, Authorization")
	ctx.Header("Access-Control-Allow-Methods", "POST, GET, OPTIONS")
	ctx.Header("Access-Control-Expose-Headers", "Content-Length, Access-Control-Allow-Origin, Access-Control-Allow-Headers, Content-Type")
	ctx.Header("Access-Control-Allow-Credentials", "true")
	if ctx.Request.Method == "OPTIONS" {
		ctx.JSON(http.StatusOK, "ok")
		return
	}
	ctx.Next()
}

func main() {
	g := gin.Default()
	io := socket.NewServer(nil, nil)
	io.Of("/user", nil).On("connection", func(clients ...any) {
		log.Println("connect")
		client := clients[0].(*socket.Socket)
		client.On("ping", func(datas ...any) {
			log.Println("heart")
			client.Emit("pong", "pong")
		})
		client.On("disconnect", func(...any) {
			log.Println("disconnect")
		})
	})
	sock := io.ServeHandler(nil)
	g.Use(cross)
	g.GET("/socket.io/", gin.WrapH(sock))
	g.POST("/socket.io/", gin.WrapH(sock))
	g.Run(":5005")
}
```

**redis adapter** : Complete code in this [http.Handler](https://github.com/shitingbao/socket.io_go/blob/shitingbao/example/redis_adapter_example.go)

```golang
	io := socket.NewServer(nil, nil)

	rdsAdapter, err := redis.NewRedisAdapter(
		redis.WithRedisAddress("127.0.0.1:6379"),
	)
	if err != nil {
		log.Println(err)
		return
	}
	io.SetAdapter(rdsAdapter)
	namespace := io.Of("/", nil)

	namespace.On("connection", func(clients ...any) {
		log.Println("connect")
		client := clients[0].(*socket.Socket)
		client.On("ping", func(datas ...any) {
			log.Println("heart")
			client.Emit("pong", "pong")
		})

		client.On("broadcast", func(datas ...any) {
			// example datas is [map[event:test message:asdf room:stb]]
			da, ok := datas[0].(map[string]interface{})
			if !ok {
				client.Emit("error", "data err")
				return
			}
			log.Println("da==:", da["event"], da["message"], da["room"])
			// io.To(socket.Room(da["room"].(string))).Emit("test", da["message"])
			// you can broadcast
			io.To(socket.Room(da["room"].(string))).Timeout(1000*time.Millisecond).Emit("test", da["message"])

			// or with ack
			// @review not supported at the moment
			// io.To(socket.Room(da["room"].(string))).Timeout(1000*time.Millisecond).Emit("test", da["message"], func(msg []any, err error) {
			// 	log.Println("ack rollback==:", msg, err)
			// })

		})
		client.On("users", func(datas ...any) {
			// get all socket
			// example datas is room
			room, ok := datas[0].(string)
			if !ok {
				client.Emit("error", "data err")
				return
			}
			fs := io.In(socket.Room(room)).FetchSockets()
			ids := []socket.SocketId{}
			fs(func(sks []*socket.RemoteSocket, err error) {
				if err != nil {
					log.Println(err)
					return
				}
				for _, sck := range sks {
					// log.Println("Handshake=:", sck.Handshake())
					ids = append(ids, sck.Id())
				}
			})
			client.Emit("pong", ids)
		})
		client.On("join-room", func(datas ...any) {
			// example datas is room
			log.Println("join-room datas:", datas)
			room, ok := datas[0].(string)
			if !ok {
				client.Emit("error", "data err")
				return
			}

			// You can use `socket id` to join a room
			// The id is not used directly to find the corresponding connection object.
			// It's because this `socket id` is added to a room with this id as the key when connecting,
			// and the id is guaranteed to be unique.
			// There is only this connection in this unique room,
			// and then it is added to your corresponding room.
			// for details, see the _onconnect method of the socket object
			io.In(socket.Room(client.Id())).SocketsJoin(socket.Room(room))
			// client.Join(socket.Room(room))
			// when you join room,can broadcast other room
		})

		client.On("leave-room", func(datas ...any) {
			// example datas is room
			log.Println("leave-room datas:", datas)
			room, ok := datas[0].(string)
			if !ok {
				client.Emit("error", "data err")
				return
			}

			// client.Leave()
			// or
			io.In(socket.Room(client.Id())).SocketsLeave(socket.Room(room))
			// or leave room
			// client.Leave(socket.Room(room))
		})
		client.On("disconnect", func(...any) {
			log.Println("disconnect")
		})
	})

```

## Documentation

Please see the documentation [here](https://pkg.go.dev/github.com/zishang520/socket.io).

## Debug / logging

In order to see all the debug output, run your app with the environment variable
`DEBUG` including the desired scope.

To see the output from all of Socket.IO's debugging scopes you can use:

```
DEBUG=socket.io*
```

## Testing

```
make test
```

## License

[MIT](LICENSE)
