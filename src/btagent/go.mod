module btagent

go 1.26.0

require (
	github.com/godbus/dbus/v5 v5.2.2
	leuchtfeuer/lfbus v0.0.0
)

require golang.org/x/sys v0.48.0 // indirect

replace leuchtfeuer/lfbus => ../lfbus
