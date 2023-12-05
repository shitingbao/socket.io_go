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
	// 白名单自定义
	allowedOrigins := []string{"http://192.168.31.33:3001", "http://192.168.31.33:3000"}
	origin := ctx.Request.Header.Get("Origin")
	// log.Println("origin=:", origin, " Referer:", ctx.Request.Referer()) origin or Referer
	for _, allowedOrigin := range allowedOrigins {
		if origin == allowedOrigin {
			ctx.Writer.Header().Set("Access-Control-Allow-Origin", allowedOrigin)
			break
		}
	}

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
	// go ExampleRedisAdapterNode(":8000")

	// the other node
	// these node can can discover each other in redisAdapterTest's system
	// redisAdapterTest is my example serverName
	// go ExampleRedisAdapterNode(":8001")
	// ...
	// ...
	// other node
	ExampleRedisAdapterNode(":8000")
	//
	// ...
	// ...
}

func ExampleRedisAdapterNode(address string) {
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
			// fs := client.Nsp().FetchSockets()
			fs := io.FetchSockets()
			ids := []socket.SocketId{}
			fs(func(sks []*socket.RemoteSocket, err error) {
				for _, sck := range sks {
					ids = append(ids, sck.Id())
				}
			})
			log.Println("join-room:", ids)
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
	g.Run(address)
}
