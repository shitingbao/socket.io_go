package main

import (
	"context"
	"log"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"github.com/zishang520/engine.io/v2/utils"
	"github.com/zishang520/socket.io-go-redis/adapter"
	rtypes "github.com/zishang520/socket.io-go-redis/types"
	"github.com/zishang520/socket.io/v2/socket"
)

// the zishang520 redis adapter example
func OtherExampleRedisAdapterNode(address string) {
	g := gin.Default()

	// srv is listen's address or http server
	// opts *ServerOptions
	io := socket.NewServer(nil, nil)

	redisClient := rtypes.NewRedisClient(context.Background(), redis.NewClient(&redis.Options{
		Addr:     "localhost:6379",
		Username: "",
		Password: "",
		DB:       0,
	}))
	redisClient.On("error", func(errors ...any) {
		utils.Log().Error("Error: %v", errors)
	})
	io.SetAdapter(&adapter.RedisAdapterBuilder{
		Redis: redisClient,
		Opts:  &adapter.RedisAdapterOptions{},
	})
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
			// io.To(socket.Room(da["room"].(string))).Timeout(1000*time.Millisecond).Emit("test", da["message"])

			// or with ack
			// @review not supported at the moment
			io.To(socket.Room(da["room"].(string))).Timeout(1000*time.Millisecond).Emit("test", da["message"], func(msg []any, err error) {
				log.Println("ack rollback==:", msg, err)
			})

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
	sock := io.ServeHandler(nil)

	g.Use(cross)
	g.GET("/socket.io/", gin.WrapH(sock))
	g.POST("/socket.io/", gin.WrapH(sock))
	g.Run(address)
}
