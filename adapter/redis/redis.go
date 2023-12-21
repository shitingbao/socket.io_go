package redis

import (
	"encoding/json"
	"errors"
	"log"
	"strings"

	"github.com/google/uuid"
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
	r.adapter = new(socket.AdapterBuilder).New(nsp)
	r.rooms = &types.Map[socket.Room, *types.Set[socket.SocketId]]{}
	r.sids = &types.Map[socket.SocketId, *types.Set[socket.Room]]{}
	r.encoder = nsp.Server().Encoder()
	r._broadcast = r.broadcast

	prefix := "socket.io"
	r.channel = prefix + "#" + nsp.Name() + "#"
	r.requestChannel = prefix + "-request#" + nsp.Name() + "#"
	r.responseChannel = prefix + "-response#" + nsp.Name() + "#"
	r.specificResponseChannel =
		r.responseChannel + r.serverId + "#"

	r.redisListeners.Store("psub", func(msg, channel string) {
		r.onmessage(channel, msg)
	})
	r.redisListeners.Store("sub", func(msg, channel string) {
		r.onrequest(channel, msg)
	})

	psub := r.rdb.PSubscribe(r.ctx, r.channel+"*") //  r.redisListeners["psub"]
	r.PSubs = append(r.PSubs, psub)
	go r.run("psub", psub)
	sub := r.rdb.Subscribe(r.ctx, r.requestChannel, r.responseChannel, r.specificResponseChannel)
	r.Subs = append(r.Subs, sub)
	go r.run("sub", sub)
	return r
}

func (r *RedisAdapter) Init() {
}

func (r *RedisAdapter) Rooms() *types.Map[socket.Room, *types.Set[socket.SocketId]] {
	return r.adapter.Rooms()
}

func (r *RedisAdapter) Sids() *types.Map[socket.SocketId, *types.Set[socket.Room]] {
	return r.adapter.Sids()
}

func (r *RedisAdapter) Nsp() socket.NamespaceInterface {
	return r.nsp
}

// To be overridden
func (r *RedisAdapter) Close() {

	for _, c := range r.PSubs {
		c.PUnsubscribe(r.ctx, r.channel+"*")
	}

	for _, c := range r.Subs {
		c.Unsubscribe(r.ctx, r.requestChannel, r.responseChannel, r.specificResponseChannel)
	}
}

// Returns the number of Socket.IO servers in the cluster
// Number of subscriptions to requestChannel
func (r *RedisAdapter) ServerCount() int64 {
	val := r.rdb.PubSubNumSub(r.ctx, r.requestChannel).Val()
	return val[r.requestChannel]
}

// Adds a socket to a list of room.
func (r *RedisAdapter) AddAll(id socket.SocketId, rooms *types.Set[socket.Room]) {
	r.adapter.AddAll(id, rooms)

	request := HandMessagePool.Get().(*HandMessage)
	request.Uid = r.uid
	request.Type = REMOTE_JOIN
	request.Opts = &socket.BroadcastOptions{
		Rooms: rooms,
	}
	request.Rooms = rooms.Keys()
	defer HandMessagePool.Put(request)
	r.rdb.Publish(r.ctx, r.requestChannel, request)
}

// Removes a socket from a room.
func (r *RedisAdapter) Del(id socket.SocketId, room socket.Room) {
	r.adapter.Del(id, room)
}

// Removes a socket from all rooms it's joined.
func (r *RedisAdapter) DelAll(id socket.SocketId) {
	r.adapter.DelAll(id)
}

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
	packet.Nsp = r.Nsp().Name()
	onlyLocal := false
	if opts != nil && opts.Flags != nil && opts.Flags.Local {
		onlyLocal = true
	}
	if !onlyLocal {
		rawOpts := socket.BroadcastOptions{
			Rooms:  (opts.Rooms),
			Except: opts.Except,
			Flags:  opts.Flags,
		}
		msg := r.parser.encode([]any{r.uid, packet, rawOpts})
		channel := r.channel
		if opts.Rooms.Len() == 1 {
			channel += string(opts.Rooms.Keys()[0]) + "#"
		}
		r.rdb.Publish(r.ctx, channel, msg)
	}
	r.adapter.Broadcast(packet, opts)
}

// Broadcasts a packet and expects multiple acknowledgements.
//
// Options:
//   - `Flags` {*BroadcastFlags} flags for this packet
//   - `Except` {*types.Set[Room]} sids that should be excluded
//   - `Rooms` {*types.Set[Room]} list of rooms to broadcast to
func (r *RedisAdapter) BroadcastWithAck(packet *parser.Packet, opts *socket.BroadcastOptions, clientCountCallback func(uint64), ack func([]any, error)) {
	packet.Nsp = r.Nsp().Name()
	onlyLocal := opts.Flags.Local
	requestId := uuid.New().String()
	if !onlyLocal {
		rawOpts := socket.BroadcastOptions{
			Rooms:  opts.Rooms,
			Except: opts.Except,
			Flags:  opts.Flags,
		}

		request := HandMessagePool.Get().(*HandMessage)
		request.Uid = r.uid
		request.RequestId = requestId
		request.Type = BROADCAST
		request.Packet = packet
		request.Opts = &rawOpts
		defer HandMessagePool.Put(request)

		r.rdb.Publish(r.ctx, r.requestChannel, request)
		req := &ackRequest{
			clientCountCallbackFun: clientCountCallback,
			ackFun:                 ack,
		}
		r.ackRequests.Store(requestId, req)
	}

	r.adapter.BroadcastWithAck(packet, opts, clientCountCallback, ack)
}

// Gets a list of sockets by sid.
func (r *RedisAdapter) Sockets(room *types.Set[socket.Room]) *types.Set[socket.SocketId] {
	return r.adapter.Sockets(room)
}

// Gets the list of rooms a given socket has joined.
func (r *RedisAdapter) SocketRooms(id socket.SocketId) *types.Set[socket.Room] {
	return r.adapter.SocketRooms(id)
}

// Returns the matching socket instances
func (r *RedisAdapter) FetchSockets(opts *socket.BroadcastOptions) func(func([]socket.SocketDetails, error)) {
	localSockets := r.adapter.FetchSockets(opts)
	if opts.Flags.Local {
		return localSockets
	}
	numSub := r.ServerCount()
	if numSub <= 1 {
		return localSockets
	}
	lsockets := []socket.SocketDetails{}
	localSockets(func(sds []socket.SocketDetails, err error) {
		lsockets = append(lsockets, sds...)
	})
	requestId := uuid.New().String()
	rawOpts := socket.BroadcastOptions{
		Rooms:  opts.Rooms,
		Except: opts.Except,
	}

	putRequest := HandMessagePool.Get().(*HandMessage)
	putRequest.Uid = r.uid
	putRequest.RequestId = requestId
	putRequest.Type = REMOTE_FETCH
	putRequest.Opts = &rawOpts
	return func(f func(sockets []socket.SocketDetails, err error)) {
		defer HandMessagePool.Put(putRequest)
		mesChan := make(chan socket.SocketDetails, 1)
		localRequest := HandMessagePool.Get().(*HandMessage)
		localRequest.Type = REMOTE_FETCH
		localRequest.NumSub = numSub
		localRequest.MsgCount = 1
		localRequest.Sockets = lsockets
		localRequest.Channal = mesChan
		r.requests.Store(requestId, localRequest)
		err := r.rdb.Publish(r.ctx, r.requestChannel, putRequest).Err()
		// @review 加入超时,超时后在这里清理 requests
		sockets := []socket.SocketDetails{}

		for sk := range mesChan {
			sockets = append(sockets, sk)
		}
		// r.apply(opts, func(socket *socket.Socket) {
		// 	sockets = append(sockets, socket)
		// })
		f(sockets, err)
	}
}

// Makes the matching socket instances join the specified rooms
func (r *RedisAdapter) AddSockets(opts *socket.BroadcastOptions, rooms []socket.Room) {
	if opts.Flags.Local {
		r.adapter.AddSockets(opts, rooms)
		return
	}
	request := HandMessagePool.Get().(*HandMessage)
	request.Uid = r.uid
	request.Type = REMOTE_JOIN
	request.Opts = &socket.BroadcastOptions{
		Rooms:  opts.Rooms,
		Except: opts.Except,
	}
	request.Rooms = rooms
	defer HandMessagePool.Put(request)
	r.rdb.Publish(r.ctx, r.requestChannel, request)
}

// Makes the matching socket instances leave the specified rooms
func (r *RedisAdapter) DelSockets(opts *socket.BroadcastOptions, rooms []socket.Room) {
	if opts.Flags.Local {
		r.adapter.DelSockets(opts, rooms)
		return
	}
	request := HandMessagePool.Get().(*HandMessage)
	request.Uid = r.uid
	request.Type = REMOTE_LEAVE
	request.Opts = &socket.BroadcastOptions{
		Rooms:  opts.Rooms,
		Except: opts.Except,
	}
	request.Rooms = rooms
	defer HandMessagePool.Put(request)
	r.rdb.Publish(r.ctx, r.requestChannel, request)
}

// Makes the matching socket instances disconnect
func (r *RedisAdapter) DisconnectSockets(opts *socket.BroadcastOptions, close bool) {
	if opts.Flags.Local {
		r.adapter.DisconnectSockets(opts, close)
		return
	}

	request := HandMessagePool.Get().(*HandMessage)
	request.Uid = r.uid
	request.Type = REMOTE_DISCONNECT
	request.Opts = &socket.BroadcastOptions{
		Rooms:  opts.Rooms,
		Except: opts.Except,
	}
	request.Close = close
	defer HandMessagePool.Put(request)

	r.rdb.Publish(r.ctx, r.requestChannel, request)
}

// Send a packet to the other Socket.IO servers in the cluster
// this is globe packet
func (r *RedisAdapter) ServerSideEmit(packet []any) error {
	_, ok := packet[len(packet)-1].(func([]any))
	if ok {
		return r.serverSideEmitWithAck(packet)
	}

	request := HandMessagePool.Get().(*HandMessage)
	request.Uid = r.uid
	request.Type = SERVER_SIDE_EMIT
	request.Data = packet
	defer HandMessagePool.Put(request)
	return r.rdb.Publish(r.ctx, r.requestChannel, request).Err()
}

func (r *RedisAdapter) serverSideEmitWithAck(packet []any) error {
	ack, ok := packet[len(packet)-1].(func(...any))
	if !ok {
		return errors.New("packet func err")
	}
	numSub := r.ServerCount() - 1
	if numSub <= 0 {
		ack(nil)
		return nil
	}

	requestId := uuid.New().String()

	request := HandMessagePool.Get().(*HandMessage)
	request.Uid = r.uid
	request.RequestId = requestId
	request.Type = SERVER_SIDE_EMIT
	request.Data = packet
	defer HandMessagePool.Put(request)

	// timeoutId := r.task.Set(r.requestsTimeout, func() {
	// 	storeRequest := r.requests[requestId]
	// 	if storeRequest != nil {
	// 		ack(errors.New("timeout reached"), storeRequest.Responses)
	// 	}
	// 	delete(r.requests, requestId)
	// })

	putRequest := HandMessagePool.Get().(*HandMessage)
	putRequest.Type = SERVER_SIDE_EMIT
	putRequest.NumSub = numSub
	defer HandMessagePool.Put(putRequest)

	return r.rdb.Publish(r.ctx, r.requestChannel, request).Err()

}

// Save the client session in order to restore it upon reconnection.
func (r *RedisAdapter) PersistSession(s *socket.SessionToPersist) {
	r.adapter.PersistSession(s)
}

// Restore the session and find the packets that were missed by the client.
func (r *RedisAdapter) RestoreSession(id socket.PrivateSessionId, pack string) (*socket.Session, error) {
	return r.adapter.RestoreSession(id, pack)
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
		rd, ok := r.redisListeners.Load(listen)
		if !ok {
			continue
		}
		listener := rd.(func(string, string))
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
	args := HandMessage{}
	if err := json.Unmarshal([]byte(msg), &args); err != nil {
		return
	}
	if args.Uid == r.serverId {
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
	r.adapter.Broadcast(args.Packet, args.Opts)
}

// 收到其他节点请求后的应答
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
	request := HandMessagePool.Get().(*HandMessage)
	defer HandMessagePool.Put(request)
	if err := json.Unmarshal([]byte(msg), request); err != nil {
		log.Println(err)
		return
	}

	response := HandMessagePool.Get().(*HandMessage)
	defer HandMessagePool.Put(request)

	response.RequestId = request.RequestId

	switch request.Type {
	case SOCKETS:
		if _, ok := r.requests.Load(request.RequestId); ok {
			return
		}
		rms := &types.Set[socket.Room]{}
		for _, v := range request.Rooms {
			rms.Add(v)
		}
		response.SocketIds = r.Sockets(rms)
		r.publishResponse(request.RequestId, response)
	case ALL_ROOMS:
		if _, ok := r.requests.Load(request.RequestId); ok {
			return
		}
		response.Rooms = r.Rooms().Keys()
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
		r.publishResponse(request.RequestId, response)
	case REMOTE_FETCH:
		if _, ok := r.requests.Load(request.RequestId); ok {
			return
		}
		localSockets := r.adapter.FetchSockets(request.Opts)
		socketFetch := func(sockets []socket.SocketDetails, err error) {
			response.Sockets = append(response.Sockets, sockets...)
		}
		localSockets(socketFetch)
		r.publishResponse(request.RequestId, response)
	case SERVER_SIDE_EMIT:
		if request.Uid == r.uid {
			return
		}
		// withAck := request.RequestId
		// if withAck != "" {
		// 	// r.nsp.EmitUntyped()
		// }
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
			_, ok := r.ackRequests.Load(request.RequestId)
			if ok {
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
				// response.Type = BROADCAST_CLIENT_COUNT
				// response.Packet = arg
				r.publishResponse(request.RequestId, response)
			})
		}
	default:
		log.Println("ignoring unknown request type: ", request.Type)
	}
}

// 收到自己节点发出请求的应答消息
func (r *RedisAdapter) onresponse(channel, msg string) {
	response := HandMessage{}
	if err := json.Unmarshal([]byte(msg), &response); err != nil {
		return
	}
	requestId := response.RequestId

	acq, ok := r.ackRequests.Load(requestId)
	if ok {
		ackRequest := acq.(AckRequest)
		switch response.Type {
		case BROADCAST_CLIENT_COUNT:
			ackRequest.clientCountCallback(response.ClientCount)
		case BROADCAST_ACK:
			ackRequest.ack([]any{response.Packet}, nil)
		}
		return
	}
	res, ok := r.requests.Load(requestId)
	request := res.(*HandMessage) // Store everything obtained in this request, and delete it after all is obtained.

	_, ackOk := r.ackRequests.Load(requestId)
	if requestId == "" || !(ok || ackOk) {
		return
	}
	switch request.Type {
	case SOCKETS:
	case REMOTE_FETCH:
		request.MsgCount++
		if response.Sockets == nil {
			return
		}
		request.Sockets = append(request.Sockets, response.Sockets...)
		for _, s := range response.Sockets {
			request.Channal <- s
		}
		if request.MsgCount == request.NumSub { // NumSub is the number of service nodes
			// r.task.Clear(request.TimeoutId)
			// @review
			// if request.Resolve != nil {
			// 	request.Resolve(request.Sockets)
			// }
			close(request.Channal)
			r.requests.Delete(requestId)
		}
	case ALL_ROOMS:
		request.MsgCount++
		if response.Rooms == nil {
			return
		}
		request.Rooms = append(request.Rooms, response.Rooms...)
		if request.MsgCount == request.NumSub {
			// r.task.Clear(request.TimeoutId)
			// @review
			// if request.Resolve != nil {
			// 	request.Resolve(request.Rooms)
			// }
			r.requests.Delete(requestId)
		}

	case REMOTE_JOIN:
	case REMOTE_LEAVE:
	case REMOTE_DISCONNECT:
		// r.task.Clear(request.TimeoutId)
		// @review
		// if request.Resolve != nil {
		// 	request.Resolve()
		// }
		r.requests.Delete(requestId)
	case SERVER_SIDE_EMIT:
		request.Responses = append(request.Responses, response.Packet.Data)
		if int64(len(request.Responses)) == request.NumSub {
			// r.task.Clear(request.TimeoutId)
			// @review
			// if request.Resolve != nil {
			// 	request.Resolve(request.Responses...)
			// }
			r.requests.Delete(requestId)
		}
	default:
		log.Println("ignoring unknown request type: ", request.Type)
	}
}

func (r *RedisAdapter) publishResponse(requestId string, response any) {
	responseChannel := r.responseChannel + "$" + requestId + "#"
	if !r.publishOnSpecificResponseChannel {
		responseChannel = r.responseChannel
	}
	r.rdb.Publish(r.ctx, responseChannel, response)
}
