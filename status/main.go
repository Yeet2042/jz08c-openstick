// jz-status draws a status screen on the JZ08C's 128x128 TFT (/dev/fb0).
//
// It shows battery, time, 4G signal and operator, IP addresses and the number
// of WiFi clients, and toggles the screen (backlight) on a short press of the
// power button. Data comes straight from D-Bus, netlink, nl80211 and sysfs,
// so no helper processes are spawned.
package main

import (
	"encoding/binary"
	"flag"
	"image/png"
	"log"
	"os"
	"strings"
	"time"
)

const (
	fbPath    = "/dev/fb0"
	backlight = "/sys/class/backlight/jz08c-bl/brightness"
	ledsDir   = "/sys/class/leds"
)

// buttons whose short press toggles the screen (matched by input device name)
var buttonNames = []string{"pm8941_pwrkey", "GPIO Buttons"}

func setBacklight(on bool) {
	v := "0"
	if on {
		v = "1"
	}
	if err := writeFile(backlight, v); err != nil {
		log.Printf("backlight: %v", err)
	}
}

// ledsOff keeps the indicator LEDs dark (they are too bright to be useful).
func ledsOff() {
	for _, l := range []string{"red:status", "green:status", "green:aux"} {
		_ = writeFile(ledsDir+"/"+l+"/trigger", "none")
		_ = writeFile(ledsDir+"/"+l+"/brightness", "0")
	}
}

// openButtons finds the evdev nodes of the board's buttons in /proc/bus/input/devices.
func openButtons() []*os.File {
	var files []*os.File
	name := ""
	for _, line := range strings.Split(readFile("/proc/bus/input/devices"), "\n") {
		switch {
		case strings.HasPrefix(line, "N: Name="):
			name = strings.Trim(strings.TrimPrefix(line, "N: Name="), `"`)
		case strings.HasPrefix(line, "H: Handlers="):
			for _, want := range buttonNames {
				if name != want {
					continue
				}
				for _, h := range strings.Fields(strings.TrimPrefix(line, "H: Handlers=")) {
					if strings.HasPrefix(h, "event") {
						if f, err := os.Open("/dev/input/" + h); err == nil {
							files = append(files, f)
							log.Printf("watching %s (%s)", f.Name(), name)
						}
					}
				}
			}
		}
	}
	return files
}

// watchButton sends on presses for every key-down event (struct input_event, 24 bytes on arm64).
func watchButton(f *os.File, presses chan<- struct{}) {
	buf := make([]byte, 24*16)
	for {
		n, err := f.Read(buf)
		if err != nil {
			log.Printf("%s: %v", f.Name(), err)
			return
		}
		for i := 0; i+24 <= n; i += 24 {
			typ := binary.LittleEndian.Uint16(buf[i+16:])
			val := int32(binary.LittleEndian.Uint32(buf[i+20:]))
			if typ == 1 && val == 1 { // EV_KEY, pressed
				presses <- struct{}{}
			}
		}
	}
}

func collect(mr *modemReader, wr *wifiReader) Status {
	pct, usb := battery()
	return Status{
		Host:    hostname(),
		Battery: pct,
		USB:     usb,
		Modem:   mr.Read(),
		IPs:     [3]string{ipv4("usb0"), ipv4("wlan0"), ipv4("wwan0")},
		Clients: wr.Clients(),
	}
}

func main() {
	interval := flag.Duration("interval", 5*time.Second, "refresh interval")
	snapshot := flag.String("png", "", "render one frame to this PNG file and exit (no framebuffer needed)")
	flag.Parse()
	log.SetFlags(0)

	mr, wr := newModemReader(), newWifiReader()

	if *snapshot != "" {
		f, err := os.Create(*snapshot)
		if err != nil {
			log.Fatal(err)
		}
		defer f.Close()
		if err := png.Encode(f, render(collect(mr, wr))); err != nil {
			log.Fatal(err)
		}
		return
	}

	fb, err := os.OpenFile(fbPath, os.O_WRONLY, 0)
	if err != nil {
		log.Fatal(err)
	}
	frame := make([]byte, W*H*4)
	blank := make([]byte, W*H*4)

	ledsOff()
	screenOn := true
	setBacklight(true)

	presses := make(chan struct{}, 8)
	for _, f := range openButtons() {
		go watchButton(f, presses)
	}

	tick := time.NewTicker(*interval)
	defer tick.Stop()
	draw := func() {
		toXRGB(render(collect(mr, wr)), frame)
		if _, err := fb.WriteAt(frame, 0); err != nil {
			log.Printf("framebuffer: %v", err)
		}
	}
	draw()
	for {
		select {
		case <-tick.C:
			if screenOn {
				draw()
			}
		case <-presses:
			screenOn = !screenOn
			setBacklight(screenOn)
			if screenOn {
				draw()
			} else {
				_, _ = fb.WriteAt(blank, 0) // black too, in case the backlight module is missing
			}
			log.Printf("button: screen on=%v", screenOn)
		}
	}
}
