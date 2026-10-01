package main

import (
	"testing"

	"github.com/godbus/dbus/v5"
)

func dev(addr, alias string, uuids []string, class uint32, paired, conn bool) map[string]map[string]dbus.Variant {
	d := map[string]dbus.Variant{
		"Address": dbus.MakeVariant(addr), "Alias": dbus.MakeVariant(alias),
		"Paired": dbus.MakeVariant(paired), "Connected": dbus.MakeVariant(conn),
	}
	if uuids != nil {
		d["UUIDs"] = dbus.MakeVariant(uuids)
	}
	if class != 0 {
		d["Class"] = dbus.MakeVariant(class)
	}
	return map[string]map[string]dbus.Variant{"org.bluez.Device1": d}
}

func TestSinkDevices(t *testing.T) {
	objs := map[dbus.ObjectPath]map[string]map[string]dbus.Variant{
		"/org/bluez/hci0":       {"org.bluez.Adapter1": {}},
		"/org/bluez/hci0/dev_1": dev("aa:bb:cc:dd:ee:01", "Küchenbox", []string{"0000110B-0000-1000-8000-00805F9B34FB"}, 0, true, true),
		"/org/bluez/hci0/dev_2": dev("AA:BB:CC:DD:EE:02", "Neue Box", nil, 0x240414, false, false), // nur Geräteklasse Audio/Video
		"/org/bluez/hci0/dev_3": dev("AA:BB:CC:DD:EE:03", "Handy", []string{"0000110a-0000-1000-8000-00805f9b34fb"}, 0x5a020c, true, true),
		"/org/bluez/hci0/dev_4": dev("AA:BB:CC:DD:EE:04", "Tastatur", nil, 0x2540, false, false),
	}
	got := sinkDevices(objs)
	if len(got) != 2 {
		t.Fatalf("%+v", got)
	}
	if got[0].Addr != "AA:BB:CC:DD:EE:01" || !got[0].Paired || !got[0].Connected || got[0].Name != "Küchenbox" {
		t.Fatalf("gekoppelte zuerst, Adresse groß: %+v", got[0])
	}
	if got[1].Name != "Neue Box" || got[1].Paired {
		t.Fatalf("%+v", got[1])
	}
}

func TestDevPath(t *testing.T) {
	*adapter = "hci0"
	if p := devPath("aa:bb:cc:dd:ee:01"); p != "/org/bluez/hci0/dev_AA_BB_CC_DD_EE_01" {
		t.Fatal(p)
	}
}
