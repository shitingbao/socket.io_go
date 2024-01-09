# redis adapter

see from [socket.io-redis-adapter](https://github.com/socketio/socket.io-redis-adapter)

## 1.communication method

1.1 Communication between nodes use the [redis subscription](https://redis.io/docs/interact/pubsub/) model  
1.2 And Multi-node layout uses nginx.see https://socket.io/zh-CN/docs/v4/reverse-proxy/#nginx or see nginx_config_test file;  
This is just a simple example. For specific situations, you need to add logic to your project yourself.


# error

`_onpacket` function no ack function reback in cluder node in the cluster
