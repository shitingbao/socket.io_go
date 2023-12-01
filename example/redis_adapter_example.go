package main

import (
	"log"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/zishang520/socket.io/v2/adapter/redis"
	"github.com/zishang520/socket.io/v2/socket"
)

// if my system name is redisAdapterTest,
// you can generate a unique identifier yourself
// or write it into a configuration file
// to ensure that nodes in the system can discover each other.
var serverName = "redisAdapterTest"

func cross(ctx *gin.Context) {
	origin := ctx.Request.Header.Get("Origin")
	ctx.Writer.Header().Set("Access-Control-Allow-Origin", origin)
	// ctx.Header("Access-Control-Allow-Origin", "http://localhost:3001,http://localhost:3000")
	ctx.Header("Access-Control-Allow-Headers", "Content-Type,AccessToken,X-CSRF-Token, Authorization,x-device-sn,x-device-token")
	ctx.Header("Access-Control-Allow-Methods", "POST, GET, OPTIONS")
	ctx.Header("Access-Control-Expose-Headers", "Content-Length, Access-Control-Allow-Origin, Access-Control-Allow-Headers, Content-Type,x-device-sn")
	ctx.Header("Access-Control-Allow-Credentials", "true")
	if ctx.Request.Method == "OPTIONS" {
		ctx.JSON(http.StatusOK, "ok")
		return
	}
	ctx.Next()
}

func ExampleRedisAdapter() {
	// one node
	// go OtherNodeExampleRedisAdapter()

	// two node
	go ExampleRedisAdapterNode()
	OtherNodeExampleRedisAdapter()
	// ...
	// ...
	// other node
	// ...
	// ...
}

func ExampleRedisAdapterNode() {
	g := gin.Default()

	// srv is listen's address or http server
	// opts *ServerOptions
	io := socket.NewServer(nil, nil)

	rdsAdapter, err := redis.NewRedisAdapter(
		redis.WithRedisAddress("127.0.0.1:6379"),
	)
	if err != nil {
		log.Println(err)
		return
	}
	io.SetAdapter(rdsAdapter)
	io.Of("/", nil).On("connection", func(clients ...any) {
		log.Println("connect")
		client := clients[0].(*socket.Socket)
		client.On("ping", func(datas ...any) {
			log.Println("heart")
			client.Emit("pong", "pong")
		})
		client.On("join-room", func(datas ...any) {
			das, ok := datas[0].(string)
			if !ok {
				client.Emit("error", "data err")
				return
			}
			client.Join(socket.Room(das))
			fs := io.FetchSockets()
			fs(func(sks []*socket.RemoteSocket, err error) {
				for _, sck := range sks {
					log.Println("id=:", sck.Id(), err)
					log.Println("rooms=:", sck.Rooms(), err)
					log.Println("Handshake=:", sck.Handshake().Query, err)
				}
			})
			log.Println("join-room:", datas)
			client.Emit("join-room", "pong")
		})

		client.On("disconnect", func(...any) {
			log.Println("disconnect")
		})
	})
	sock := io.ServeHandler(nil)

	g.Use(cross)
	g.GET("/socket.io/", gin.WrapH(sock))
	g.POST("/socket.io/", gin.WrapH(sock))
	g.Run(":8000")
}

// the other node
// these node can can discover each other in redisAdapterTest's system
// redisAdapterTest is my example serverName
func OtherNodeExampleRedisAdapter() {
	g := gin.Default()

	// srv is listen's address or http server
	// opts *ServerOptions
	io := socket.NewServer(nil, nil)

	rdsAdapter, err := redis.NewRedisAdapter(
		redis.WithRedisAddress("127.0.0.1:6379"),
	)
	if err != nil {
		log.Println(err)
		return
	}
	io.SetAdapter(rdsAdapter)
	io.Of("/", nil).On("connection", func(clients ...any) {
		log.Println("connect")
		client := clients[0].(*socket.Socket)
		client.On("ping", func(datas ...any) {
			log.Println("heart")
			client.Emit("pong", "pong")
		})
		client.On("join-room", func(datas ...any) {
			das, ok := datas[0].(string)
			if !ok {
				client.Emit("error", "data err")
				return
			}
			client.Join(socket.Room(das))
			fs := io.FetchSockets()
			fs(func(sks []*socket.RemoteSocket, err error) {
				for _, sck := range sks {
					log.Println("8001 id=:", sck.Id(), err)
					log.Println("8001 rooms=:", sck.Rooms(), err)
					log.Println("8001 Handshake=:", sck.Handshake().Query, err)
				}
			})
			log.Println("join-room:", datas)
			client.Emit("join-room", "pong")
		})

		client.On("disconnect", func(...any) {
			log.Println("disconnect")
		})
	})
	sock := io.ServeHandler(nil)

	g.Use(cross)
	g.GET("/socket.io/", gin.WrapH(sock))
	g.POST("/socket.io/", gin.WrapH(sock))
	g.Run(":8001")
}
