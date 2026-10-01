module btagent

go 1.22

require (
	github.com/godbus/dbus/v5 v5.2.2
	leuchtfeuer/wamp v0.0.0
)

require (
	github.com/vmihailenco/msgpack/v5 v5.4.1 // indirect
	github.com/vmihailenco/tagparser/v2 v2.0.0 // indirect
	golang.org/x/sys v0.27.0 // indirect
)

replace leuchtfeuer/wamp => ../wamp
