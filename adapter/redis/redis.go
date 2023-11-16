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
		listener := r.redisListeners["sub"]
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
	// switch request.Type {
	// case SOCKETS:
	// 	if r.requests[request.RequestId] != nil {
	// 		return
	// 	}
	// 	sockets := r.apply()
	// }
	// let response, socket;

	// switch (request.type) {
	//   case RequestType.SOCKETS:
	//     if (this.requests.has(request.requestId)) {
	//       return;
	//     }

	//     const sockets = await super.sockets(new Set(request.rooms));

	//     response = JSON.stringify({
	//       requestId: request.requestId,
	//       sockets: [...sockets],
	//     });

	//     this.publishResponse(request, response);
	//     break;

	//   case RequestType.ALL_ROOMS:
	//     if (this.requests.has(request.requestId)) {
	//       return;
	//     }

	//     response = JSON.stringify({
	//       requestId: request.requestId,
	//       rooms: [...this.rooms.keys()],
	//     });

	//     this.publishResponse(request, response);
	//     break;

	//   case RequestType.REMOTE_JOIN:
	//     if (request.opts) {
	//       const opts = {
	//         rooms: new Set<Room>(request.opts.rooms),
	//         except: new Set<Room>(request.opts.except),
	//       };
	//       return super.addSockets(opts, request.rooms);
	//     }

	//     socket = this.nsp.sockets.get(request.sid);
	//     if (!socket) {
	//       return;
	//     }

	//     socket.join(request.room);

	//     response = JSON.stringify({
	//       requestId: request.requestId,
	//     });

	//     this.publishResponse(request, response);
	//     break;

	//   case RequestType.REMOTE_LEAVE:
	//     if (request.opts) {
	//       const opts = {
	//         rooms: new Set<Room>(request.opts.rooms),
	//         except: new Set<Room>(request.opts.except),
	//       };
	//       return super.delSockets(opts, request.rooms);
	//     }

	//     socket = this.nsp.sockets.get(request.sid);
	//     if (!socket) {
	//       return;
	//     }

	//     socket.leave(request.room);

	//     response = JSON.stringify({
	//       requestId: request.requestId,
	//     });

	//     this.publishResponse(request, response);
	//     break;

	//   case RequestType.REMOTE_DISCONNECT:
	//     if (request.opts) {
	//       const opts = {
	//         rooms: new Set<Room>(request.opts.rooms),
	//         except: new Set<Room>(request.opts.except),
	//       };
	//       return super.disconnectSockets(opts, request.close);
	//     }

	//     socket = this.nsp.sockets.get(request.sid);
	//     if (!socket) {
	//       return;
	//     }

	//     socket.disconnect(request.close);

	//     response = JSON.stringify({
	//       requestId: request.requestId,
	//     });

	//     this.publishResponse(request, response);
	//     break;

	//   case RequestType.REMOTE_FETCH:
	//     if (this.requests.has(request.requestId)) {
	//       return;
	//     }

	//     const opts = {
	//       rooms: new Set<Room>(request.opts.rooms),
	//       except: new Set<Room>(request.opts.except),
	//     };
	//     const localSockets = await super.fetchSockets(opts);

	//     response = JSON.stringify({
	//       requestId: request.requestId,
	//       sockets: localSockets.map((socket) => {
	//         // remove sessionStore from handshake, as it may contain circular references
	//         const { sessionStore, ...handshake } = socket.handshake;
	//         return {
	//           id: socket.id,
	//           handshake,
	//           rooms: [...socket.rooms],
	//           data: socket.data,
	//         };
	//       }),
	//     });

	//     this.publishResponse(request, response);
	//     break;

	//   case RequestType.SERVER_SIDE_EMIT:
	//     if (request.uid === this.uid) {
	//       debug("ignore same uid");
	//       return;
	//     }
	//     const withAck = request.requestId !== undefined;
	//     if (!withAck) {
	//       this.nsp._onServerSideEmit(request.data);
	//       return;
	//     }
	//     let called = false;
	//     const callback = (arg) => {
	//       // only one argument is expected
	//       if (called) {
	//         return;
	//       }
	//       called = true;
	//       debug("calling acknowledgement with %j", arg);
	//       this.pubClient.publish(
	//         this.responseChannel,
	//         JSON.stringify({
	//           type: RequestType.SERVER_SIDE_EMIT,
	//           requestId: request.requestId,
	//           data: arg,
	//         })
	//       );
	//     };
	//     request.data.push(callback);
	//     this.nsp._onServerSideEmit(request.data);
	//     break;

	//   case RequestType.BROADCAST: {
	//     if (this.ackRequests.has(request.requestId)) {
	//       // ignore self
	//       return;
	//     }

	//     const opts = {
	//       rooms: new Set<Room>(request.opts.rooms),
	//       except: new Set<Room>(request.opts.except),
	//     };

	//     super.broadcastWithAck(
	//       request.packet,
	//       opts,
	//       (clientCount) => {
	//         debug("waiting for %d client acknowledgements", clientCount);
	//         this.publishResponse(
	//           request,
	//           JSON.stringify({
	//             type: RequestType.BROADCAST_CLIENT_COUNT,
	//             requestId: request.requestId,
	//             clientCount,
	//           })
	//         );
	//       },
	//       (arg) => {
	//         debug("received acknowledgement with value %j", arg);

	//         this.publishResponse(
	//           request,
	//           this.parser.encode({
	//             type: RequestType.BROADCAST_ACK,
	//             requestId: request.requestId,
	//             packet: arg,
	//           })
	//         );
	//       }
	//     );
	//     break;
	//   }

	//   default:
	//     debug("ignoring unknown request type: %s", request.type);
	// }
}

func (r *RedisAdapter) onresponse(channel, msg string) {
	// let response;
	// response:=""
	//   // if the buffer starts with a "{" character
	// args := bindMessage{}
	// if err := json.Unmarshal([]byte(msg), &args); err != nil {
	// 	return
	// }

	// const requestId = response.requestId;

	// if (this.ackRequests.has(requestId)) {
	//   const ackRequest = this.ackRequests.get(requestId);

	//   switch (response.type) {
	//     case RequestType.BROADCAST_CLIENT_COUNT: {
	//       ackRequest?.clientCountCallback(response.clientCount);
	//       break;
	//     }

	//     case RequestType.BROADCAST_ACK: {
	//       ackRequest?.ack(response.packet);
	//       break;
	//     }
	//   }
	//   return;
	// }

	// if (
	//   !requestId ||
	//   !(this.requests.has(requestId) || this.ackRequests.has(requestId))
	// ) {
	//   debug("ignoring unknown request");
	//   return;
	// }

	// debug("received response %j", response);

	// const request = this.requests.get(requestId);

	// switch (request.type) {
	//   case RequestType.SOCKETS:
	//   case RequestType.REMOTE_FETCH:
	//     request.msgCount++;

	//     // ignore if response does not contain 'sockets' key
	//     if (!response.sockets || !Array.isArray(response.sockets)) return;

	//     if (request.type === RequestType.SOCKETS) {
	//       response.sockets.forEach((s) => request.sockets.add(s));
	//     } else {
	//       response.sockets.forEach((s) => request.sockets.push(s));
	//     }

	//     if (request.msgCount === request.numSub) {
	//       clearTimeout(request.timeout);
	//       if (request.resolve) {
	//         request.resolve(request.sockets);
	//       }
	//       this.requests.delete(requestId);
	//     }
	//     break;

	//   case RequestType.ALL_ROOMS:
	//     request.msgCount++;

	//     // ignore if response does not contain 'rooms' key
	//     if (!response.rooms || !Array.isArray(response.rooms)) return;

	//     response.rooms.forEach((s) => request.rooms.add(s));

	//     if (request.msgCount === request.numSub) {
	//       clearTimeout(request.timeout);
	//       if (request.resolve) {
	//         request.resolve(request.rooms);
	//       }
	//       this.requests.delete(requestId);
	//     }
	//     break;

	//   case RequestType.REMOTE_JOIN:
	//   case RequestType.REMOTE_LEAVE:
	//   case RequestType.REMOTE_DISCONNECT:
	//     clearTimeout(request.timeout);
	//     if (request.resolve) {
	//       request.resolve();
	//     }
	//     this.requests.delete(requestId);
	//     break;

	//   case RequestType.SERVER_SIDE_EMIT:
	//     request.responses.push(response.data);

	//     debug(
	//       "serverSideEmit: got %d responses out of %d",
	//       request.responses.length,
	//       request.numSub
	//     );
	//     if (request.responses.length === request.numSub) {
	//       clearTimeout(request.timeout);
	//       if (request.resolve) {
	//         request.resolve(null, request.responses);
	//       }
	//       this.requests.delete(requestId);
	//     }
	//     break;

	//   default:
	//     debug("ignoring unknown request type: %s", request.type);
	// }
}
