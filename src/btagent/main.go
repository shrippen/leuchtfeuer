// btagent: BlueZ-Agent für den Invoke als reinen Lautsprecher ohne Bedienelemente.
//   - nimmt jede Kopplung an (NoInputNoOutput / Just Works) und jede Dienstautorisierung,
//   - markiert gekoppelte Geräte als vertraut, damit sie sich selbst wieder verbinden dürfen,
//   - hält den Adapter eingeschaltet, sichtbar und koppelbereit (ohne Zeitlimit).
//
// Läuft dauerhaft und holt alles nach, wenn bluetoothd neu startet.
package main

import (
	"flag"
	"log"
	"time"

	"github.com/godbus/dbus/v5"
)

const (
	agentPath = dbus.ObjectPath("/invoke/agent")
	bluez     = "org.bluez"
)

var (
	name    = flag.String("name", "HK Invoke", "Anzeigename")
	adapter = flag.String("adapter", "hci0", "Adapter")
	bus     *dbus.Conn
)

type agent struct{}

func (agent) Release() *dbus.Error { return nil }
func (agent) RequestPinCode(d dbus.ObjectPath) (string, *dbus.Error) {
	log.Printf("PIN angefragt von %s", d)
	return "0000", nil
}
func (agent) DisplayPinCode(d dbus.ObjectPath, pin string) *dbus.Error { return nil }
func (agent) RequestPasskey(d dbus.ObjectPath) (uint32, *dbus.Error)   { return 0, nil }
func (agent) DisplayPasskey(d dbus.ObjectPath, p uint32, e uint16) *dbus.Error {
	return nil
}
func (agent) RequestConfirmation(d dbus.ObjectPath, p uint32) *dbus.Error {
	log.Printf("Kopplung bestätigt: %s", d)
	trust(d)
	return nil
}
func (agent) RequestAuthorization(d dbus.ObjectPath) *dbus.Error {
	log.Printf("Kopplung autorisiert: %s", d)
	trust(d)
	return nil
}
func (agent) AuthorizeService(d dbus.ObjectPath, uuid string) *dbus.Error {
	log.Printf("Dienst %s erlaubt für %s", uuid, d)
	return nil
}
func (agent) Cancel() *dbus.Error { return nil }

func setProp(path dbus.ObjectPath, iface, prop string, v any) error {
	return bus.Object(bluez, path).Call("org.freedesktop.DBus.Properties.Set", 0, iface, prop, dbus.MakeVariant(v)).Err
}

func trust(dev dbus.ObjectPath) {
	if err := setProp(dev, "org.bluez.Device1", "Trusted", true); err != nil {
		log.Printf("Trusted %s: %v", dev, err)
	}
}

func registerAgent() {
	mgr := bus.Object(bluez, "/org/bluez")
	if err := mgr.Call("org.bluez.AgentManager1.RegisterAgent", 0, agentPath, "NoInputNoOutput").Err; err != nil {
		if e, ok := err.(dbus.Error); ok && e.Name == "org.bluez.Error.AlreadyExists" {
			return
		}
		return // bluetoothd noch nicht da; nächster Durchlauf
	}
	if err := mgr.Call("org.bluez.AgentManager1.RequestDefaultAgent", 0, agentPath).Err; err != nil {
		log.Printf("RequestDefaultAgent: %v", err)
		return
	}
	log.Printf("Agent registriert (NoInputNoOutput)")
}

// tick gleicht Adapter-Einstellungen und Vertrauensliste ab.
func tick() {
	registerAgent()
	path := dbus.ObjectPath("/org/bluez/" + *adapter)
	want := map[string]any{
		"Powered": true, "Alias": *name, "Discoverable": true,
		"DiscoverableTimeout": uint32(0), "Pairable": true, "PairableTimeout": uint32(0),
	}
	var props map[string]dbus.Variant
	if err := bus.Object(bluez, path).Call("org.freedesktop.DBus.Properties.GetAll", 0, "org.bluez.Adapter1").Store(&props); err != nil {
		return
	}
	// Powered zuerst, die übrigen Werte verlangen einen eingeschalteten Adapter
	for _, k := range []string{"Powered", "Alias", "Pairable", "PairableTimeout", "Discoverable", "DiscoverableTimeout"} {
		if cur, ok := props[k]; !ok || cur.Value() != want[k] {
			if err := setProp(path, "org.bluez.Adapter1", k, want[k]); err != nil {
				log.Printf("%s setzen: %v", k, err)
			} else {
				log.Printf("%s = %v", k, want[k])
			}
		}
	}
	var objs map[dbus.ObjectPath]map[string]map[string]dbus.Variant
	if err := bus.Object(bluez, "/").Call("org.freedesktop.DBus.ObjectManager.GetManagedObjects", 0).Store(&objs); err != nil {
		return
	}
	for p, ifs := range objs {
		d, ok := ifs["org.bluez.Device1"]
		if !ok {
			continue
		}
		if paired, _ := d["Paired"].Value().(bool); paired {
			if tr, _ := d["Trusted"].Value().(bool); !tr {
				log.Printf("Gerät %v vertraut", d["Address"].Value())
				trust(p)
			}
		}
	}
}

func main() {
	flag.Parse()
	var err error
	for {
		if bus, err = dbus.ConnectSystemBus(); err == nil {
			break
		}
		log.Printf("D-Bus: %v", err)
		time.Sleep(3 * time.Second)
	}
	if err := bus.Export(agent{}, agentPath, "org.bluez.Agent1"); err != nil {
		log.Fatal(err)
	}
	go volumeLoop()
	for {
		tick()
		time.Sleep(2 * time.Second)
	}
}
