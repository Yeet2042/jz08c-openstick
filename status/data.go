package main

import (
	"net"
	"os"
	"strconv"
	"strings"

	"github.com/godbus/dbus/v5"
	"github.com/mdlayher/wifi"
)

// ---- modem (ModemManager over D-Bus) ----

const (
	mmService = "org.freedesktop.ModemManager1"
	mmPath    = "/org/freedesktop/ModemManager1"
	mmModem   = "org.freedesktop.ModemManager1.Modem"
	mm3gpp    = "org.freedesktop.ModemManager1.Modem.Modem3gpp"

	mmStateFailed    = -1
	mmStateConnected = 11
	mmFailedSIM      = 2 // MM_MODEM_STATE_FAILED_REASON_SIM_MISSING
)

var mmStateNames = map[int32]string{
	-1: "failed", 0: "unknown", 1: "initializing", 2: "locked", 3: "disabled",
	4: "disabling", 5: "enabling", 6: "enabled", 7: "searching", 8: "registered",
	9: "disconnecting", 10: "connecting", 11: "connected",
}

type Modem struct {
	Present  bool
	State    int32
	Failed   uint32
	Signal   int
	Tech     string
	Operator string
}

func (m Modem) StateName() string {
	if s, ok := mmStateNames[m.State]; ok {
		return s
	}
	return "?"
}

// accessTech turns the MMModemAccessTechnology bitmask into "4G"/"3G"/"2G".
func accessTech(mask uint32) string {
	switch {
	case mask&(1<<15) != 0:
		return "5G"
	case mask&(1<<14) != 0:
		return "4G"
	case mask&(0x1f<<5) != 0: // UMTS, HSDPA, HSUPA, HSPA, HSPA+
		return "3G"
	case mask&(0x0f<<1) != 0: // GSM, GSM compact, GPRS, EDGE
		return "2G"
	}
	return ""
}

type modemReader struct {
	conn *dbus.Conn
}

func newModemReader() *modemReader {
	conn, err := dbus.SystemBus()
	if err != nil {
		return &modemReader{}
	}
	return &modemReader{conn: conn}
}

// Read fetches every modem property with a single GetManagedObjects call.
func (r *modemReader) Read() Modem {
	if r.conn == nil {
		return Modem{}
	}
	var objs map[dbus.ObjectPath]map[string]map[string]dbus.Variant
	err := r.conn.Object(mmService, mmPath).
		Call("org.freedesktop.DBus.ObjectManager.GetManagedObjects", 0).Store(&objs)
	if err != nil {
		return Modem{}
	}
	for _, ifaces := range objs {
		props, ok := ifaces[mmModem]
		if !ok {
			continue
		}
		m := Modem{Present: true}
		if v, ok := props["State"].Value().(int32); ok {
			m.State = v
		}
		if v, ok := props["StateFailedReason"].Value().(uint32); ok {
			m.Failed = v
		}
		if v, ok := props["AccessTechnologies"].Value().(uint32); ok {
			m.Tech = accessTech(v)
		}
		// SignalQuality is a (uint32 quality, bool recent) struct
		if v, ok := props["SignalQuality"].Value().([]interface{}); ok && len(v) > 0 {
			if q, ok := v[0].(uint32); ok {
				m.Signal = int(q)
			}
		}
		if p3, ok := ifaces[mm3gpp]; ok {
			if v, ok := p3["OperatorName"].Value().(string); ok {
				m.Operator = v
			}
		}
		return m
	}
	return Modem{}
}

// ---- network ----

// ipv4 returns the first IPv4 address of dev. On usb0 it skips the fixed
// fallback address so the DHCP lease from the host is shown.
func ipv4(dev string) string {
	ifi, err := net.InterfaceByName(dev)
	if err != nil {
		return "-"
	}
	addrs, err := ifi.Addrs()
	if err != nil {
		return "-"
	}
	first := ""
	for _, a := range addrs {
		ipn, ok := a.(*net.IPNet)
		if !ok || ipn.IP.To4() == nil {
			continue
		}
		ip := ipn.IP.String()
		if first == "" {
			first = ip
		}
		if !strings.HasPrefix(ip, "192.168.6.") {
			return ip
		}
	}
	if first == "" {
		return "-"
	}
	return first
}

type wifiReader struct {
	c *wifi.Client
}

func newWifiReader() *wifiReader {
	c, err := wifi.New()
	if err != nil {
		return &wifiReader{}
	}
	return &wifiReader{c: c}
}

// Clients counts stations associated to wlan0 (nl80211 station dump).
func (w *wifiReader) Clients() int {
	if w.c == nil {
		return 0
	}
	ifis, err := w.c.Interfaces()
	if err != nil {
		return 0
	}
	for _, ifi := range ifis {
		if ifi.Name == "wlan0" {
			st, err := w.c.StationInfo(ifi)
			if err != nil {
				return 0
			}
			return len(st)
		}
	}
	return 0
}

// ---- battery / sysfs ----

func readFile(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

func writeFile(path, val string) error {
	return os.WriteFile(path, []byte(val), 0)
}

// battery returns the charge in percent (-1 if unknown) and whether USB power is present.
func battery() (int, bool) {
	pct := -1
	if v, err := strconv.Atoi(readFile("/sys/class/power_supply/pm8916-bms-vm/capacity")); err == nil {
		pct = v
	}
	return pct, readFile("/sys/class/power_supply/pm8916-lbc-chgr/online") == "1"
}
