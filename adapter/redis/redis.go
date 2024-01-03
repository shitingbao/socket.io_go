package redis

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"strconv"
	"strings"
	"sync/atomic"

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
		r.responseChannel + r.uid + "#"

	r.redisListeners.Store("psub", func(channel, msg string) {
		if err := r.onmessage(channel, msg); err != nil {
			log.Println(err)
		}
	})
	r.redisListeners.Store("sub", func(channel, msg string) {
		if err := r.onrequest(channel, msg); err != nil {
			log.Println(err)
		}
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
	defer request.Recycle()
	r.publishRequest(r.requestChannel, request.LocalHandMessage)
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
		rawOpts := &socket.BroadcastOptions{
			Rooms:  (opts.Rooms),
			Except: opts.Except,
			Flags:  opts.Flags,
		}
		msg := &HandMessage{}
		msg.Uid = r.uid
		msg.Packet = packet
		msg.Opts = rawOpts
		channel := r.channel
		if opts.Rooms.Len() == 1 {
			channel += string(opts.Rooms.Keys()[0]) + "#"
		}
		r.publishRequest(channel, msg.LocalHandMessage)
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
	if !onlyLocal {
		requestId := RequestIdPool.Get()
		defer RequestIdPool.Put(requestId)
		rawOpts := socket.BroadcastOptions{
			Rooms:  opts.Rooms,
			Except: opts.Except,
			Flags:  opts.Flags,
		}

		request := HandMessagePool.Get().(*HandMessage)
		request.Uid = r.uid
		request.RequestId = requestId.(string)
		request.Type = BROADCAST
		request.Packet = packet
		request.Opts = &rawOpts
		defer request.Recycle()
		r.publishRequest(r.requestChannel, request.LocalHandMessage)
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
	serverCount := r.ServerCount()
	if serverCount <= 1 {
		return localSockets
	}
	lsockets := []socket.SocketDetails{}
	localSockets(func(sds []socket.SocketDetails, err error) {
		lsockets = append(lsockets, sds...)
	})
	requestId := RequestIdPool.Get()
	rawOpts := socket.BroadcastOptions{
		Rooms:  opts.Rooms,
		Except: opts.Except,
	}

	putRequest := HandMessagePool.Get().(*HandMessage)
	putRequest.Uid = r.uid
	putRequest.RequestId = requestId.(string)
	putRequest.Type = REMOTE_FETCH
	putRequest.Opts = &rawOpts
	return func(f func(sockets []socket.SocketDetails, err error)) {
		defer putRequest.Recycle()
		mesChan := make(chan RemoteSocket, 1)
		localRequest := HandMessagePool.Get().(*HandMessage)
		localRequest.Type = REMOTE_FETCH
		localRequest.MsgCount = atomic.Int32{}
		localRequest.MsgCount.Add(1)

		localRequest.Channal = mesChan
		r.requests.Store(requestId, localRequest)
		defer func() {
			r.requests.Delete(requestId)
			RequestIdPool.Put(requestId)
		}()
		sockets := []socket.SocketDetails{}

		if err := r.publishRequest(r.requestChannel, putRequest.LocalHandMessage); err != nil {
			f(sockets, err)
			return
		}
		sockets = append(sockets, lsockets...)
		c, _ := context.WithTimeout(r.ctx, r.requestsTimeout)
		flag := false
		for {
			select {
			case sk, ok := <-mesChan:
				if !ok {
					// 关闭说明获取到所有节点的数据
					flag = true
					break
				}
				l := &localRemoteSocket{
					id:        sk.Id,
					handshake: sk.Handshake,
					rooms:     sk.Rooms,
					data:      sk.Data,
				}
				sockets = append(sockets, l)
			case <-c.Done():
				// 超时不再等待直接反馈
				flag = true
			}
			if flag {
				break
			}
		}
		f(sockets, nil)
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
	defer request.Recycle()
	r.publishRequest(r.requestChannel, request.LocalHandMessage)
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
	defer request.Recycle()
	r.publishRequest(r.requestChannel, request.LocalHandMessage)
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
	defer request.Recycle()

	r.publishRequest(r.requestChannel, request.LocalHandMessage)
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
	defer request.Recycle()
	return r.publishRequest(r.requestChannel, request.LocalHandMessage)
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

	requestId := RequestIdPool.Get()
	defer RequestIdPool.Put(requestId)
	request := HandMessagePool.Get().(*HandMessage)
	request.Uid = r.uid
	request.RequestId = requestId.(string)
	request.Type = SERVER_SIDE_EMIT
	request.Data = packet
	defer request.Recycle()

	putRequest := HandMessagePool.Get().(*HandMessage)
	putRequest.Type = SERVER_SIDE_EMIT
	defer putRequest.Recycle()
	return r.publishRequest(r.requestChannel, request.LocalHandMessage)

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

func (r *RedisAdapter) onmessage(channel, msg string) error {
	if !strings.HasPrefix(channel, r.channel) {
		return errors.New("not channel")
	}
	room := channel[len(r.channel):]
	if room != "" {
		return errors.New("room is nil")
	}
	args := HandMessage{}
	if err := json.Unmarshal([]byte(msg), &args); err != nil {
		return err
	}
	if args.Uid == r.uid {
		return errors.New("sample uid")
	}
	if args.Packet.Nsp == "" {
		args.Packet.Nsp = "/"
	}
	if args.Packet.Nsp != r.nsp.Name() {
		return errors.New("Nsp Name not sample")
	}

	args.Opts.Rooms = &types.Set[socket.Room]{}
	args.Opts.Except = &types.Set[socket.Room]{}
	r.adapter.Broadcast(args.Packet, args.Opts)
	return nil
}

// 向其他节点发送请求
func (r *RedisAdapter) onrequest(channel, msg string) error {
	if strings.HasPrefix(channel, r.responseChannel) {
		return r.onresponse(channel, msg)
	}
	if !strings.HasPrefix(channel, r.requestChannel) {
		return errors.New("not request channel")
	}
	request := HandMessagePool.Get().(*HandMessage)
	defer request.Recycle()
	if err := json.Unmarshal([]byte(msg), request); err != nil {
		return err
	}

	response := HandMessagePool.Get().(*HandMessage)
	defer response.Recycle()
	response.RequestId = request.RequestId

	switch request.Type {
	case SOCKETS:
		if _, ok := r.requests.Load(request.RequestId); ok {
			return nil
		}
		rms := &types.Set[socket.Room]{}
		for _, v := range request.Rooms {
			rms.Add(v)
		}
		response.SocketIds = r.Sockets(rms)
		return r.publishResponse(request, response)
	case ALL_ROOMS:
		if _, ok := r.requests.Load(request.RequestId); ok {
			return nil
		}
		response.Rooms = r.Rooms().Keys()
		return r.publishResponse(request, response)
	case REMOTE_JOIN:
		if request.Opts != nil {
			r.adapter.AddSockets(request.Opts, request.Rooms)
			return nil
		}
		socket, ok := r.nsp.Sockets().Load(request.Sid)
		if !ok {
			return nil
		}
		socket.Join(request.Rooms...)
		return r.publishResponse(request, response)
	case REMOTE_LEAVE:
		if request.Opts != nil {
			r.DelSockets(request.Opts, request.Rooms)
			return nil
		}
		socket, ok := r.nsp.Sockets().Load(request.Sid)
		if !ok {
			return nil
		}
		if len(request.Rooms) > 0 {
			socket.Leave(request.Rooms[0])
		}
		return r.publishResponse(request, response)
	case REMOTE_DISCONNECT:
		if request.Opts != nil {
			r.DisconnectSockets(request.Opts, request.Close)
			return nil
		}
		socket, ok := r.nsp.Sockets().Load(request.Sid)
		if !ok {
			return nil
		}
		socket.Disconnect(request.Close)
		return r.publishResponse(request, response)
	case REMOTE_FETCH:
		if _, ok := r.requests.Load(request.RequestId); ok {
			return nil
		}
		localSockets := r.adapter.FetchSockets(request.Opts)
		socketFetch := func(sockets []socket.SocketDetails, err error) {
			for _, sd := range sockets {
				s := RemoteSocket{
					Id:        sd.Id(),
					Handshake: sd.Handshake(),
					Rooms:     sd.Rooms(),
					Data:      sd.Data(),
				}
				response.Sockets = append(response.Sockets, s)
			}
		}
		localSockets(socketFetch)
		return r.publishResponse(request, response)
	case SERVER_SIDE_EMIT:
		if request.Uid == r.uid {
			return nil
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
				return nil
			}
			opt := &socket.BroadcastOptions{
				Rooms:  request.Opts.Rooms,
				Except: request.Opts.Except,
			}
			r.BroadcastWithAck(request.Packet, opt, func(clientCount uint64) {
				response := HandMessagePool.Get().(*HandMessage)
				defer response.Recycle()
				response.Type = BROADCAST_CLIENT_COUNT
				response.RequestId = request.RequestId
				response.ClientCount = clientCount

				//  struct {
				// 	Type        SocketDataType
				// 	RequestId   string
				// 	ClientCount uint64
				// }{Type: BROADCAST_CLIENT_COUNT,
				// 	RequestId:   request.RequestId,
				// 	ClientCount: clientCount,
				// }

				r.publishResponse(request, response)
			}, func(arg []any, err error) {
				// response.Type = BROADCAST_CLIENT_COUNT
				// response.Packet = arg
				r.publishResponse(request, response)
			})
		}
	default:
		return errors.New("ignoring unknown onrequest type: " + strconv.Itoa(int(request.Type)))
	}
	return nil
}

// 在 onrequest 发送后 收到自己节点发出请求的应答消息处理
func (r *RedisAdapter) onresponse(channel, msg string) error {
	response := HandMessagePool.Get().(*HandMessage)
	defer response.Recycle()
	if err := json.Unmarshal([]byte(msg), response); err != nil {
		return err
	}
	requestId := response.RequestId
	acq, ok := r.ackRequests.Load(requestId)
	if ok {
		ackRequest, ok := acq.(AckRequest)
		if !ok {
			return nil
		}
		switch response.Type {
		case BROADCAST_CLIENT_COUNT:
			ackRequest.clientCountCallback(response.ClientCount)
		case BROADCAST_ACK:
			ackRequest.ack([]any{response.Packet}, nil)
		}
		return nil
	}
	res, ok := r.requests.Load(requestId)
	request, hk := res.(*HandMessage) // Store everything obtained in this request, and delete it after all is obtained.
	if !hk {
		return nil
	}
	_, ackOk := r.ackRequests.Load(requestId)
	if requestId == "" || !(ok || ackOk) {
		return nil
	}
	switch request.Type {
	case SOCKETS:
	case REMOTE_FETCH:
		for _, s := range response.Sockets {
			request.Channal <- s
		}
		request.MsgCount.Add(1)
		if int64(request.MsgCount.Load()) == r.ServerCount() { // NumSub is the number of service nodes
			if request.CloseFlag.CompareAndSwap(0, 1) {
				// 注意多个节点中，最后两个节点在反馈 socket 时，可能同时结束，注意只有一个 close
				close(request.Channal)
			}
		}
	case ALL_ROOMS:
		request.MsgCount.Add(1)
		if response.Rooms == nil {
			return nil
		}
		request.Rooms = append(request.Rooms, response.Rooms...)
		if int64(request.MsgCount.Load()) == r.ServerCount() {
			// @review
			// if request.Resolve != nil {
			// 	request.Resolve(request.Rooms)
			// }
			r.requests.Delete(requestId)
		}

	case REMOTE_JOIN:
	case REMOTE_LEAVE:
	case REMOTE_DISCONNECT:
		// @review
		// if request.Resolve != nil {
		// 	request.Resolve()
		// }
		r.requests.Delete(requestId)
	case SERVER_SIDE_EMIT:
		request.Responses = append(request.Responses, response.Packet.Data)
		if int64(len(request.Responses)) == r.ServerCount() {
			// @review
			// if request.Resolve != nil {
			// 	request.Resolve(request.Responses...)
			// }
			r.requests.Delete(requestId)
		}
	default:
		return errors.New("ignoring unknown onresponse type: " + strconv.Itoa(int(request.Type)))
	}
	return nil
}

func (r *RedisAdapter) publishResponse(request *HandMessage, response *HandMessage) error {
	responseChannel := r.responseChannel + "$" + request.Uid + "#"
	if !r.publishOnSpecificResponseChannel {
		responseChannel = r.responseChannel
	}
	b, err := json.Marshal(response.LocalHandMessage)
	if err != nil {
		return err
	}
	return r.rdb.Publish(r.ctx, responseChannel, b).Err()
}

func (r *RedisAdapter) publishRequest(channel string, mes LocalHandMessage) error {
	b, err := json.Marshal(mes)
	if err != nil {
		return err
	}
	return r.rdb.Publish(r.ctx, channel, b).Err()
}
