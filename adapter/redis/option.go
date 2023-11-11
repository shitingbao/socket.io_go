package redis

import (
	"context"
	"errors"

	"github.com/redis/go-redis/v9"
	"github.com/zishang520/engine.io/events"
	"github.com/zishang520/engine.io/types"
	"github.com/zishang520/socket.io-go-parser/parser"
	"github.com/zishang520/socket.io/v2/socket"
)

type option struct {
	Address    string
	Passsword  string
	ServerName string
	Db         int
}

type Option func(*option)

type RedisAdapter struct {
	events.EventEmitter

	// serverName should be a unique identifier in the system to guide all nodes to join their own services.
	serverName string
	rdb        *redis.Client
	ctx        context.Context

	nsp     socket.NamespaceInterface
	rooms   *types.Map[socket.Room, *types.Set[socket.SocketId]]
	sids    *types.Map[socket.SocketId, *types.Set[socket.Room]]
	encoder parser.Encoder

	_broadcast func(*parser.Packet, *socket.BroadcastOptions)
}

// WithRedisAddress eg : 127.0.0.1:6379
func WithRedisAddress(ads string) Option {
	return func(o *option) {
		o.Address = ads
	}
}

func WithRedisDb(db int) Option {
	return func(o *option) {
		o.Db = db
	}
}

func WithRedisServerName(name string) Option {
	return func(o *option) {
		o.ServerName = name
	}
}

func NewRedisAdapter(opts ...Option) (*RedisAdapter, error) {
	op := &option{
		Address: "127.0.0.1:6379",
	}
	for _, o := range opts {
		o(op)
	}
	if op.ServerName == "" {
		return nil, errors.New("you should have a ‘servername’ to distinguish which system other nodes should join")
	}
	r := redis.NewClient(&redis.Options{
		Addr:     op.Address,
		Password: op.Passsword,
		DB:       op.Db,
	})
	if err := r.Ping(context.Background()).Err(); err != nil {
		return nil, err
	}
	return &RedisAdapter{
		rdb:        r,
		ctx:        context.Background(),
		serverName: op.ServerName,
	}, nil
}
