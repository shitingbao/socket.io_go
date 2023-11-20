package redis

import (
	"encoding/json"
	"log"
	"strings"

	"github.com/redis/go-redis/v9"
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

	prefix := r.serverId + "socket.io"
	r.channel = prefix + "#" + nsp.Name() + "#"
	r.requestChannel = prefix + "-request#" + nsp.Name() + "#"
	r.responseChannel = prefix + "-response#" + nsp.Name() + "#"
	r.specificResponseChannel =
		r.responseChannel + r.serverId + "#"

	r.rdb.PSubscribe(r.ctx, r.channel+"*")
	r.rdb.Subscribe(r.ctx,
		r.requestChannel,
		r.responseChannel,
		r.specificResponseChannel,
	)

	// r.pubSub = r.rdb.Subscribe(r.ctx, r.subscribeGlobeKey())
	r.redisListeners["psub"] = func(msg, channel string) {
		r.onmessage(channel, msg)
	}

	r.redisListeners["sub"] = func(msg, channel string) {
		r.onrequest(channel, msg)
	}

	psub := r.rdb.PSubscribe(r.ctx, r.channel) //  r.redisListeners["psub"]
	r.run("psub", psub)
	sub := r.rdb.Subscribe(r.ctx, r.requestChannel, r.responseChannel, r.specificResponseChannel)
	r.run("sub", sub)
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
// 每个server 都有自己的id，每次onmessage 时把 id带上，每次set 一下
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
func (r *RedisAdapter) AddSockets(socket *socket.BroadcastOptions, room []socket.Room) {
}

// Makes the matching socket instances leave the specified rooms
func (r *RedisAdapter) DelSockets(*socket.BroadcastOptions, []socket.Room) {}

// Makes the matching socket instances disconnect
func (r *RedisAdapter) DisconnectSockets(*socket.BroadcastOptions, bool) {}

// Send a packet to the other Socket.IO servers in the cluster
// this is globe packet
func (r *RedisAdapter) ServerSideEmit(packs []any) error {
	// return r.rdb.Publish(r.ctx, r.subscribeGlobeKey(), string(b)).Err()
	return nil
}

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

func (r *RedisAdapter) run(listen string, sub *redis.PubSub) {
	for {
		mes, err := sub.ReceiveMessage(r.ctx)
		if err != nil {
			log.Println(err)
			continue
		}
		listener := r.redisListeners[listen]
		listener(mes.Channel, mes.Payload)
	}
}

func (r *RedisAdapter) onmessage(channel, msg string) {
	if !strings.HasPrefix(channel, r.channel) {
		return
	}
	room := channel[len(r.channel):]
	if room != "" {
		return
	}
	args := bindMessage{}
	if err := json.Unmarshal([]byte(msg), &args); err != nil {
		return
	}
	if args.ServerId == r.serverId {
		return
	}
	if args.Packet.Nsp == "" {
		args.Packet.Nsp = "/"
	}
	if args.Packet.Nsp != r.nsp.Name() {
		return
	}

	args.Opts.Rooms = &types.Set[socket.Room]{}
	args.Opts.Except = &types.Set[socket.Room]{}
	r.Broadcast(&args.Packet, &args.Opts)
	// super.broadcast(packet, opts);
}

func (r *RedisAdapter) onrequest(channel, msg string) {
	if strings.HasPrefix(channel, r.responseChannel) {
		r.onresponse(channel, msg)
		return
	}
	if strings.HasPrefix(channel, r.requestChannel) {
		log.Println("ignore different channel")
		return
	}
	// let request;
	request := subRequest{}
	if err := json.Unmarshal([]byte(msg), &request); err != nil {
		log.Println(err)
		return
	}
	switch request.Type {
	case SOCKETS:
		if r.requests[request.RequestId] != nil {
			return
		}
		rms := &types.Set[socket.Room]{}
		for _, v := range request.Rooms {
			rms.Add(v)
		}
		sockets := r.Sockets(rms)
		response := struct {
			RequestId string
			Sockets   *types.Set[socket.SocketId]
		}{
			RequestId: request.RequestId,
			Sockets:   sockets,
		}
		r.publishResponse(request.RequestId, response)
	case ALL_ROOMS:
		if r.requests[request.RequestId] != nil {
			return
		}
		response := struct {
			RequestId string
			Rooms     []socket.Room
		}{
			RequestId: request.RequestId,
			Rooms:     r.Rooms().Keys(),
		}
		r.publishResponse(request.RequestId, response)
	case REMOTE_JOIN:
		if request.Opts != nil {
			r.AddSockets(request.Opts, request.Rooms)
			return
		}
		socket, ok := r.nsp.Sockets().Load(request.Sid)
		if !ok {
			return
		}
		socket.Join(request.Rooms...)
		response := struct {
			RequestId string
		}{
			RequestId: request.RequestId,
		}
		r.publishResponse(request.RequestId, response)

	case REMOTE_LEAVE:
		if request.Opts != nil {
			r.DelSockets(request.Opts, request.Rooms)
			return
		}
		socket, ok := r.nsp.Sockets().Load(request.Sid)
		if !ok {
			return
		}
		if len(request.Rooms) > 0 {
			socket.Leave(request.Rooms[0])
		}
		response := struct {
			RequestId string
		}{
			RequestId: request.RequestId,
		}
		r.publishResponse(request.RequestId, response)

	case REMOTE_DISCONNECT:
		if request.Opts != nil {
			r.DisconnectSockets(request.Opts, request.Close)
			return
		}
		socket, ok := r.nsp.Sockets().Load(request.Sid)
		if !ok {
			return
		}
		socket.Disconnect(request.Close)
		response := struct {
			RequestId string
		}{
			RequestId: request.RequestId,
		}
		r.publishResponse(request.RequestId, response)

	case REMOTE_FETCH:
		if r.requests[request.RequestId] != nil {
			return
		}
		type responseData struct {
			Id        socket.SocketId
			Handshake socket.Handshake
			Rooms     *types.Set[socket.Room]
			Data      any
		}
		datas := []responseData{}
		fech := r.FetchSockets(request.Opts)
		socketFetch := func(sockets []socket.SocketDetails, err error) {
			for _, sock := range sockets {
				da := responseData{
					Id:        sock.Id(),
					Handshake: *sock.Handshake(),
					Rooms:     sock.Rooms(),
					Data:      sock.Data(),
				}
				datas = append(datas, da)
			}
		}
		fech(socketFetch)
		r.publishResponse(request.RequestId, datas)
	case SERVER_SIDE_EMIT:
		if request.Uid == r.uid {
			return
		}

		withAck := request.RequestId
		if withAck != "" {
			// r.nsp.EmitUntyped()
		}
		// if (request.uid === this.uid) {
		//   debug("ignore same uid");
		//   return;
		// }
		// const withAck = request.requestId !== undefined;
		// if (!withAck) {
		//   this.nsp._onServerSideEmit(request.data);
		//   return;
		// }
		// let called = false;
		// const callback = (arg) => {
		//   // only one argument is expected
		//   if (called) {
		//     return;
		//   }
		//   called = true;
		//   debug("calling acknowledgement with %j", arg);
		//   this.pubClient.publish(
		//     this.responseChannel,
		//     JSON.stringify({
		//       type: RequestType.SERVER_SIDE_EMIT,
		//       requestId: request.requestId,
		//       data: arg,
		//     })
		//   );
		// };
		// request.data.push(callback);
		// this.nsp._onServerSideEmit(request.data);
		// break;
	case BROADCAST:
		{
			req := r.ackRequests[request.RequestId]
			if req != nil {
				return
			}
			opt := &socket.BroadcastOptions{
				Rooms:  request.Opts.Rooms,
				Except: request.Opts.Except,
			}
			r.BroadcastWithAck(request.Packet, opt, func(clientCount uint64) {
				r.publishResponse(request.RequestId, struct {
					Type        SocketDataType
					RequestId   string
					ClientCount uint64
				}{Type: BROADCAST_CLIENT_COUNT,
					RequestId:   request.RequestId,
					ClientCount: clientCount,
				})
			}, func(arg []any, err error) {
				r.publishResponse(request.RequestId, r.parser.encode(
					struct {
						Type      SocketDataType
						RequestId string
						packet    []any
					}{Type: BROADCAST_CLIENT_COUNT,
						RequestId: request.RequestId,
						packet:    arg,
					},
				))
			})
		}
	default:
		log.Printf("ignoring unknown request type: %s", request.Type)
	}
}

func (r *RedisAdapter) onresponse(channel, msg string) {
	response := subRequest{}
	if err := json.Unmarshal([]byte(msg), &response); err != nil {
		return
	}
	requestId := response.RequestId

	if r.ackRequests[requestId] != nil {
		ackRequest := r.ackRequests[requestId]
		switch response.Type {
		case BROADCAST_CLIENT_COUNT:
			ackRequest.clientCountCallback(response.ClientCount)
		case BROADCAST_ACK:
			ackRequest.ack(response.Packet)
		}
		return
	}
	if requestId == "" || !(r.requests[requestId] != nil || r.ackRequests[requestId] != nil) {
		return
	}
	request := r.requests[requestId]
	switch request.Type {
	case SOCKETS:
	case REMOTE_FETCH:
		request.MsgCount++
		if response.Sockets == nil {
			return
		}
		request.Sockets = append(request.Sockets, response.Sockets...)
		if request.MsgCount == request.NumSub {
			// clearTimeout(request.timeout)
			if request.Resolve != nil {
				request.Resolve(request.Sockets)
			}
			delete(r.requests, requestId)
		}
	case ALL_ROOMS:
		request.MsgCount++
		if response.Rooms == nil {
			return
		}
		request.Rooms = append(request.Rooms, response.Rooms...)
		if request.MsgCount == request.NumSub {
			// clearTimeout(request.timeout)
			if request.Resolve != nil {
				request.Resolve(request.Rooms)
			}
			delete(r.requests, requestId)
		}

	case REMOTE_JOIN:
	case REMOTE_LEAVE:
	case REMOTE_DISCONNECT:
		//  clearTimeout(request.timeout);
		if request.Resolve != nil {
			request.Resolve()
		}
		delete(r.requests, (requestId))
	case SERVER_SIDE_EMIT:
		request.Responses = append(request.Responses, response.Packet.Data)
		if len(request.Responses) == request.NumSub {
			// clearTimeout(request.timeout);
			if request.Resolve != nil {
				request.Resolve(request.Responses...)
			}
			delete(r.requests, requestId)
		}
	default:
		log.Println("ignoring unknown request type: %s", request.Type)
	}
}

func (r *RedisAdapter) publishResponse(requestId string, response any) {
	responseChannel := r.responseChannel + "$" + requestId + "#"
	if !r.publishOnSpecificResponseChannel {
		responseChannel = r.responseChannel
	}
	r.rdb.Publish(r.ctx, responseChannel, response)
}
