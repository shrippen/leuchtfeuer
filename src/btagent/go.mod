module btagent

go 1.22

require (
	github.com/godbus/dbus/v5 v5.2.2
	leuchtfeuer/lfbus v0.0.0
)

require golang.org/x/sys v0.27.0 // indirect

replace leuchtfeuer/lfbus => ../lfbus
