module leuchtfeuerd

go 1.26.0

require (
	github.com/caddyserver/certmagic v0.25.6
	github.com/eclipse/paho.mqtt.golang v1.5.1
	github.com/grandcat/zeroconf v1.0.0
	github.com/libdns/acmedns v0.5.0
	github.com/libdns/cloudflare v0.2.2
	github.com/libdns/desec v1.1.1
	github.com/libdns/gandi v1.1.0
	github.com/libdns/libdns v1.1.1
	github.com/libdns/namecheap v1.0.0
	github.com/libdns/porkbun v1.1.0
	github.com/mholt/acmez/v3 v3.1.7
	github.com/miekg/dns v1.1.73
	go.uber.org/zap v1.28.0
	leuchtfeuer/wamp v0.0.0
)

require (
	github.com/caddyserver/zerossl v0.1.6 // indirect
	github.com/cenkalti/backoff v2.2.1+incompatible // indirect
	github.com/gorilla/websocket v1.5.3 // indirect
	github.com/klauspost/cpuid/v2 v2.4.0 // indirect
	github.com/stretchr/testify v1.11.1 // indirect
	github.com/vmihailenco/msgpack/v5 v5.4.1 // indirect
	github.com/vmihailenco/tagparser/v2 v2.0.0 // indirect
	github.com/zeebo/blake3 v0.2.4 // indirect
	go.uber.org/multierr v1.11.0 // indirect
	go.uber.org/zap/exp v0.3.0 // indirect
	golang.org/x/crypto v0.57.0 // indirect
	golang.org/x/net v0.59.0 // indirect
	golang.org/x/sync v0.23.0 // indirect
	golang.org/x/sys v0.48.0 // indirect
	golang.org/x/text v0.42.0 // indirect
)

replace leuchtfeuer/wamp => ../wamp
