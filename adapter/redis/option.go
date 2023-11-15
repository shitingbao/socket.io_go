package redis

import (
	"context"

	"github.com/pborman/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/zishang520/engine.io/events"
	"github.com/zishang520/engine.io/types"
	"github.com/zishang520/socket.io-go-parser/parser"
	"github.com/zishang520/socket.io/v2/socket"
)

type MessageType int

const (
	INITIAL_HEARTBEAT MessageType = iota + 1
	HEARTBEAT
	BROADCAST
	SOCKETS_JOIN
	SOCKETS_LEAVE
	DISCONNECT_SOCKETS
	FETCH_SOCKETS
	FETCH_SOCKETS_RESPONSE
	SERVER_SIDE_EMIT
	SERVER_SIDE_EMIT_RESPONSE
	BROADCAST_CLIENT_COUNT
	BROADCAST_ACK
)

type option struct {
	Address                          string
	Passsword                        string
	ServerId                         string
	Db                               int
	HeartbeatInterval                int
	HeartbeatTimeout                 int
	RequestsTimeout                  int
	PublishOnSpecificResponseChannel bool
}

type Option func(*option)

type RequestType int

const (
	SOCKETS RequestType = iota
	ALL_ROOMS
	REMOTE_JOIN
	REMOTE_LEAVE
	REMOTE_DISCONNECT
	REMOTE_FETCH
	Request_SERVER_SIDE_EMIT
	Request_BROADCAST
	Request_BROADCAST_CLIENT_COUNT
	Request_BROADCAST_ACK
)

type Request struct {
	Type     RequestType
	Resolve  func()
	Timeout  int
	NumSub   int
	MsgCount int
	// [other: string]: any;
}

type friendlyErrorHandler func()

type AckRequest interface {
	clientCountCallback(clientCount int)
	ack(args ...any)
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

func WithRedisHeartbeatInterval(tm int) Option {
	return func(o *option) {
		o.HeartbeatInterval = tm
	}
}

func WithRedisHeartbeatTimeout(tm int) Option {
	return func(o *option) {
		o.HeartbeatTimeout = tm
	}
}

type RedisAdapter struct {
	events.EventEmitter

	// serverId should be a unique identifier in the system to guide all nodes to join their own services.
	serverId string

	// The number of ms between two heartbeats.
	// 5000
	HeartbeatInterval int

	// The number of ms without heartbeat before we consider a node down.
	// 10000
	HeartbeatTimeout int

	rdb *redis.Client
	ctx context.Context

	nsp     socket.NamespaceInterface
	rooms   *types.Map[socket.Room, *types.Set[socket.SocketId]]
	sids    *types.Map[socket.SocketId, *types.Set[socket.Room]]
	encoder parser.Encoder

	_broadcast func(*parser.Packet, *socket.BroadcastOptions)

	requestsTimeout                  int
	publishOnSpecificResponseChannel bool

	channel                 string
	requestChannel          string
	responseChannel         string
	specificResponseChannel string
	requests                map[string]Request
	ackRequests             map[string]AckRequest
	redisListeners          map[string](func())
	readonly                friendlyErrorHandler
	parser                  Parser
}

func NewRedisAdapter(opts ...Option) (*RedisAdapter, error) {
	op := &option{
		Address:           "127.0.0.1:6379",
		HeartbeatInterval: 5000,
		HeartbeatTimeout:  10000,
		RequestsTimeout:   5000,
	}
	for _, o := range opts {
		o(op)
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
		rdb:               r,
		ctx:               context.Background(),
		serverId:          uuid.New(),
		HeartbeatInterval: op.HeartbeatInterval,
		HeartbeatTimeout:  op.HeartbeatTimeout,

		requestsTimeout:                  op.RequestsTimeout,
		publishOnSpecificResponseChannel: op.PublishOnSpecificResponseChannel,

		requests:       make(map[string]Request),
		ackRequests:    make(map[string]AckRequest),
		redisListeners: make(map[string](func())),
		readonly:       func() {},
	}, nil
}

type ClusterMessage struct {
	ServerId string
	MType    MessageType
	Data     map[string]any
}

type Parser interface {
	decode(msg any) any
	encode(msg any) any
}

type bindMessage struct {
	ServerId string
	Packet   parser.Packet
	Opts     socket.BroadcastOptions
}
