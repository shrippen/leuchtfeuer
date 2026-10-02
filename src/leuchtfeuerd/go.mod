module leuchtfeuerd

go 1.26.0

require (
	github.com/eclipse/paho.mqtt.golang v1.5.1
	github.com/grandcat/zeroconf v1.0.0
	leuchtfeuer/wamp v0.0.0
)

require (
	github.com/cenkalti/backoff v2.2.1+incompatible // indirect
	github.com/gorilla/websocket v1.5.3 // indirect
	github.com/miekg/dns v1.1.73 // indirect
	github.com/vmihailenco/msgpack/v5 v5.4.1 // indirect
	github.com/vmihailenco/tagparser/v2 v2.0.0 // indirect
	golang.org/x/net v0.59.0 // indirect
	golang.org/x/sync v0.23.0 // indirect
	golang.org/x/sys v0.48.0 // indirect
)

replace leuchtfeuer/wamp => ../wamp
