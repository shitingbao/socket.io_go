# socket.io

## Features

**This project fork from** [socket.io](https://github.com/zishang520/socket.io)

Mainly added the function of redis adapter and how to use it

## How to use

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

Other doc Please see the origin documentation [here](https://pkg.go.dev/github.com/zishang520/socket.io).
