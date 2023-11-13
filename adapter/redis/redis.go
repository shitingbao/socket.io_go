package redis

import (
	"log"

	"github.com/zishang520/engine.io/events"
	"github.com/zishang520/engine.io/types"
	"github.com/zishang520/socket.io-go-parser/parser"
	"github.com/zishang520/socket.io/v2/socket"
)

var _ socket.Adapter = (*RedisAdapter)(nil)

func (r *RedisAdapter) New(nsp socket.NamespaceInterface) socket.Adapter {
	r.EventEmitter = events.New()
	r.nsp = nsp
	r.rooms = &types.Map[socket.Room, *types.Set[socket.SocketId]]{}
	r.sids = &types.Map[socket.SocketId, *types.Set[socket.Room]]{}
	r.encoder = nsp.Server().Encoder()
	r._broadcast = r.broadcast
	r.pubSub = r.rdb.Subscribe(r.ctx, r.serverName)
	return r
}

func (r *RedisAdapter) Init() {
}

func (r *RedisAdapter) Rooms() *types.Map[socket.Room, *types.Set[socket.SocketId]] {
	// rm := &types.Map[socket.Room, *types.Set[socket.SocketId]]{}
	// rm.Store()
	return &types.Map[socket.Room, *types.Set[socket.SocketId]]{}
}

func (r *RedisAdapter) Sids() *types.Map[socket.SocketId, *types.Set[socket.Room]] {
	return &types.Map[socket.SocketId, *types.Set[socket.Room]]{}
}

func (r *RedisAdapter) Nsp() socket.NamespaceInterface {
	return nil
}

// To be overridden
func (r *RedisAdapter) Close() {}

// Returns the number of Socket.IO servers in the cluster
func (r *RedisAdapter) ServerCount() int64 { return 0 }

// Adds a socket to a list of room.
func (r *RedisAdapter) AddAll(socket.SocketId, *types.Set[socket.Room]) {}

// Removes a socket from a room.
func (r *RedisAdapter) Del(socket.SocketId, socket.Room) {}

// Removes a socket from all rooms it's joined.
func (r *RedisAdapter) DelAll(socket.SocketId) {}

func (r *RedisAdapter) SetBroadcast(broadcast func(*parser.Packet, *socket.BroadcastOptions)) {
	r._broadcast = broadcast
}

func (r *RedisAdapter) GetBroadcast() func(*parser.Packet, *socket.BroadcastOptions) {
	return r._broadcast
}

// Broadcasts a packet.
//
// Options:
//   - `Flags` {*BroadcastFlags} flags for this packet
//   - `Except` {*types.Set[Room]} sids that should be excluded
//   - `Rooms` {*types.Set[Room]} list of rooms to broadcast to
func (r *RedisAdapter) Broadcast(packet *parser.Packet, opts *socket.BroadcastOptions) {
	r._broadcast(packet, opts)
}

// Broadcasts a packet and expects multiple acknowledgements.
//
// Options:
//   - `Flags` {*BroadcastFlags} flags for this packet
//   - `Except` {*types.Set[Room]} sids that should be excluded
//   - `Rooms` {*types.Set[Room]} list of rooms to broadcast to
func (r *RedisAdapter) BroadcastWithAck(*parser.Packet, *socket.BroadcastOptions, func(uint64), func([]any, error)) {
}

// Gets a list of sockets by sid.
func (r *RedisAdapter) Sockets(*types.Set[socket.Room]) *types.Set[socket.SocketId] {
	return &types.Set[socket.SocketId]{}
}

// Gets the list of rooms a given socket has joined.
func (r *RedisAdapter) SocketRooms(socket.SocketId) *types.Set[socket.Room] {
	return &types.Set[socket.Room]{}
}

// Returns the matching socket instances
func (r *RedisAdapter) FetchSockets(*socket.BroadcastOptions) func(func([]socket.SocketDetails, error)) {
	return func(func([]socket.SocketDetails, error)) {}
}

// Makes the matching socket instances join the specified rooms
func (r *RedisAdapter) AddSockets(*socket.BroadcastOptions, []socket.Room) {}

// Makes the matching socket instances leave the specified rooms
func (r *RedisAdapter) DelSockets(*socket.BroadcastOptions, []socket.Room) {}

// Makes the matching socket instances disconnect
func (r *RedisAdapter) DisconnectSockets(*socket.BroadcastOptions, bool) {}

// Send a packet to the other Socket.IO servers in the cluster
func (r *RedisAdapter) ServerSideEmit([]any) error { return nil }

// Save the client session in order to restore it upon reconnection.
func (r *RedisAdapter) PersistSession(*socket.SessionToPersist) {}

// Restore the session and find the packets that were missed by the client.
func (r *RedisAdapter) RestoreSession(socket.PrivateSessionId, string) (*socket.Session, error) {
	return nil, nil
}

// Broadcasts a packet.
//
// Options:
//   - `Flags` {*BroadcastFlags} flags for this packet
//   - `Except` {*types.Set[Room]} sids that should be excluded
//   - `Rooms` {*types.Set[Room]} list of rooms to broadcast to
func (r *RedisAdapter) broadcast(packet *parser.Packet, opts *socket.BroadcastOptions) {
	flags := &socket.BroadcastFlags{}
	if opts != nil && opts.Flags != nil {
		flags = opts.Flags
	}

	packetOpts := &socket.WriteOptions{}
	packetOpts.PreEncoded = true
	packetOpts.Volatile = flags.Volatile
	packetOpts.Compress = flags.Compress

	packet.Nsp = r.nsp.Name()
	encodedPackets := r.encoder.Encode(packet)
	r.apply(opts, func(socket *socket.Socket) {
		if notifyOutgoingListeners := socket.NotifyOutgoingListeners(); notifyOutgoingListeners != nil {
			notifyOutgoingListeners(packet)
		}
		socket.Client().WriteToEngine(encodedPackets, packetOpts)
	})
}

func (r *RedisAdapter) apply(opts *socket.BroadcastOptions, callback func(*socket.Socket)) {
	rooms := opts.Rooms
	except := r.computeExceptSids(opts.Except)
	if rooms != nil && rooms.Len() > 0 {
		ids := types.NewSet[socket.SocketId]()
		for _, room := range rooms.Keys() {
			if _ids, ok := r.rooms.Load(room); ok {
				for _, id := range _ids.Keys() {
					if ids.Has(id) || except.Has(id) {
						continue
					}
					if socket, ok := r.nsp.Sockets().Load(id); ok {
						callback(socket)
						ids.Add(id)
					}
				}
			}
		}
	} else {
		r.sids.Range(func(id socket.SocketId, _ *types.Set[socket.Room]) bool {
			if except.Has(id) {
				return true
			}
			if socket, ok := r.nsp.Sockets().Load(id); ok {
				callback(socket)
			}
			return true
		})
	}
}

func (r *RedisAdapter) computeExceptSids(exceptRooms *types.Set[socket.Room]) *types.Set[socket.SocketId] {
	exceptSids := types.NewSet[socket.SocketId]()
	if exceptRooms != nil && exceptRooms.Len() > 0 {
		for _, room := range exceptRooms.Keys() {
			if ids, ok := r.rooms.Load(room); ok {
				exceptSids.Add(ids.Keys()...)
			}
		}
	}
	return exceptSids
}

func (r *RedisAdapter) run() {
	for {
		mes, err := r.pubSub.ReceiveMessage(r.ctx)
		if err != nil {
			return
		}
		log.Println("mes=:", mes)
	}
}
