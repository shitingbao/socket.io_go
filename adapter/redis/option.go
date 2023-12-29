package redis

import (
	"context"
	"sync"
	"sync/atomic"
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

var (
	HandMessagePool sync.Pool
	RequestIdPool   sync.Pool
)

func init() {
	HandMessagePool.New = func() interface{} {
		return &HandMessage{}
	}

	RequestIdPool.New = func() interface{} {
		return uuid.New()
	}
}

type Option func(*option)

type SocketDataType int

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
	uid string // only uid

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

	requestsTimeout                  time.Duration // 多节点应答超时时间
	publishOnSpecificResponseChannel bool

	channel                 string
	requestChannel          string
	responseChannel         string
	specificResponseChannel string
	requests                sync.Map
	ackRequests             sync.Map
	redisListeners          sync.Map //  map[string](func(channel, msg string))
	readonly                func()
	parser                  Parser

	Subs  []*redis.PubSub
	PSubs []*redis.PubSub
}

func NewRedisAdapter(opts ...Option) (*RedisAdapter, error) {
	op := &option{
		Address:           "127.0.0.1:6379",
		HeartbeatInterval: 5000,
		HeartbeatTimeout:  10000,
		RequestsTimeout:   time.Second * 5,
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
		uid:               uuid.New(),
		HeartbeatInterval: op.HeartbeatInterval,
		HeartbeatTimeout:  op.HeartbeatTimeout,

		requestsTimeout:                  op.RequestsTimeout,
		publishOnSpecificResponseChannel: op.PublishOnSpecificResponseChannel,

		requests:       sync.Map{},
		ackRequests:    sync.Map{}, // make(map[string]AckRequest),
		redisListeners: sync.Map{}, // make(map[string](func(string, string))),
		readonly:       func() {},
	}, nil
}

type Parser interface {
	decode(msg any) any
	encode(msg any) any
}

// HandMessage 处理消息的单位
// 使用 sync pool 回收
type HandMessage struct {
	LocalHandMessage
	Channal   chan RemoteSocket `json:"channal"` // 接受其他节点反馈的内容通道
	MsgCount  atomic.Int32      `json:"msg_count"`
	CloseFlag atomic.Int32      `json:"close_flag"` // 关闭 HandMessage channel 的标志
}

type RemoteSocket struct {
	Id        socket.SocketId         `json:"id"`
	Handshake *socket.Handshake       `json:"handshake"`
	Rooms     *types.Set[socket.Room] `json:"rooms"`
	Data      any                     `json:"data"`
}

// 这一部分是 redis 通道之间传递的信息
type LocalHandMessage struct {
	Uid         string                      `json:"uid"`
	Sid         socket.SocketId             `json:"sid"`
	Type        SocketDataType              `json:"type"`
	RequestId   string                      `json:"request_id"` // 请求唯一的id
	Rooms       []socket.Room               `json:"rooms"`
	Opts        *socket.BroadcastOptions    `json:"opts"`
	Close       bool                        `json:"close"`
	Sockets     []RemoteSocket              `json:"sockets"` // bool or []socket.Socket
	SocketIds   *types.Set[socket.SocketId] `json:"socket_ids"`
	Packet      *parser.Packet              `json:"packet"`
	ClientCount uint64                      `json:"client_count"`
	Responses   []any                       `json:"responses"`
	Data        any                         `json:"data"`
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

// 用于接受远程 socket 对象数据，兼容 interface json
type localRemoteSocket struct {
	id        socket.SocketId
	handshake *socket.Handshake
	rooms     *types.Set[socket.Room]
	data      any
}

func (r *localRemoteSocket) Id() socket.SocketId {
	return r.id
}

func (r *localRemoteSocket) Handshake() *socket.Handshake {
	return r.handshake
}

func (r *localRemoteSocket) Rooms() *types.Set[socket.Room] {
	return r.rooms
}

func (r *localRemoteSocket) Data() any {
	return r.data
}

func (h *HandMessage) Recycle() {
	h.Channal = make(chan RemoteSocket, 1)
	h.MsgCount = atomic.Int32{}
	h.CloseFlag = atomic.Int32{}
	h.Uid = ""
	h.Sid = ""
	h.Type = -1
	h.RequestId = ""
	h.Rooms = []socket.Room{}
	h.Opts = &socket.BroadcastOptions{}
	h.Close = false
	h.Sockets = []RemoteSocket{}
	h.SocketIds = &types.Set[socket.SocketId]{}
	h.Packet = &parser.Packet{}
	h.ClientCount = 0
	h.Responses = []any{}
	h.Data = nil
	HandMessagePool.Put(h)
}
