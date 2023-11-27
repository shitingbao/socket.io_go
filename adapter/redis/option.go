package redis

import (
	"context"
	"time"

	"github.com/pborman/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/zishang520/engine.io/events"
	"github.com/zishang520/engine.io/types"
	"github.com/zishang520/socket.io-go-parser/parser"
	"github.com/zishang520/socket.io/v2/socket"
)

const (
	// MessageType
	INITIAL_HEARTBEAT SocketDataType = iota + 1
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

	// RequestType
	SOCKETS SocketDataType = iota + 1
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

type option struct {
	Address                          string
	Passsword                        string
	ServerId                         string
	Db                               int
	HeartbeatInterval                int
	HeartbeatTimeout                 int
	RequestsTimeout                  time.Duration
	PublishOnSpecificResponseChannel bool
}

type Option func(*option)

type SocketDataType int

type friendlyErrorHandler func()

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

	adapter socket.Adapter
	nsp     socket.NamespaceInterface
	rooms   *types.Map[socket.Room, *types.Set[socket.SocketId]]
	sids    *types.Map[socket.SocketId, *types.Set[socket.Room]]
	encoder parser.Encoder

	_broadcast func(*parser.Packet, *socket.BroadcastOptions)

	requestsTimeout                  time.Duration
	publishOnSpecificResponseChannel bool

	uid                     string // only uid
	channel                 string
	requestChannel          string
	responseChannel         string
	specificResponseChannel string
	requests                map[string]*Request
	ackRequests             map[string]AckRequest
	redisListeners          map[string](func(channel, msg string))
	readonly                friendlyErrorHandler
	parser                  Parser

	Subs  []*redis.PubSub
	PSubs []*redis.PubSub

	task Task
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

		requests:       make(map[string]*Request),
		ackRequests:    make(map[string]AckRequest),
		redisListeners: make(map[string](func(string, string))),
		readonly:       func() {},

		task: NewDefaultTask(),
	}, nil
}

type ClusterMessage struct {
	ServerId string
	MType    SocketDataType
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

type Request struct {
	Uid         string                                    `json:"uid"`
	Sid         socket.SocketId                           `json:"sid"`
	Type        SocketDataType                            `json:"type"`
	RequestId   string                                    `json:"request_id"`
	Rooms       []socket.Room                             `json:"rooms"`
	Opts        *socket.BroadcastOptions                  `json:"opts"`
	Close       bool                                      `json:"close"`
	Sockets     func(func([]socket.SocketDetails, error)) `json:"sockets"` // bool or []socket.Socket
	Packet      *parser.Packet                            `json:"packet"`
	ClientCount uint64                                    `json:"client_count"`

	Resolve   func(...any) // []socket.Socket []socket.Room,or []
	TimeoutId string       // socket timeout key,use when(delete socket by request id)
	NumSub    int64
	MsgCount  int64
	Responses []any
	Data      any
}

type AckRequest interface {
	clientCountCallback(clientCount uint64)
	ack([]any, error)
}

type ackRequest struct {
	clientCountCallbackFun func(clientCount uint64)
	ackFun                 func([]any, error)
}

func (a *ackRequest) clientCountCallback(clientCount uint64) {
	a.clientCountCallbackFun(clientCount)
}

func (a *ackRequest) ack(da []any, err error) {
	a.ackFun(da, err)
}
