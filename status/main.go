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
	"image"
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

// batOffsetMV corrects the fuel gauge voltage reading (see -bat-offset-mv).
var batOffsetMV int

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

// keyEvent is a press (down=true) or release of any of the board's buttons.
type keyEvent struct {
	down bool
	at   time.Time
}

// watchButton forwards key down/up events (struct input_event, 24 bytes on arm64).
func watchButton(f *os.File, keys chan<- keyEvent) {
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
			if typ == 1 && (val == 0 || val == 1) { // EV_KEY up/down (ignore autorepeat)
				keys <- keyEvent{down: val == 1, at: time.Now()}
			}
		}
	}
}

func collect(mr *modemReader, wr *wifiReader) Status {
	pct, mv, usb := battery()
	if mv > 0 {
		mv -= batOffsetMV
	}
	return Status{
		Host:    hostname(),
		Battery: pct,
		BattMV:  mv,
		USB:     usb,
		Modem:   mr.Read(),
		IPs:     [3]string{ipv4("usb0"), ipv4("wlan0"), ipv4("wwan0")},
		Clients: wr.Clients(),
	}
}

// how long a press has to be held to toggle the screen
const holdToggle = time.Second

// refresh interval per page: status, resources, containers
var pageInterval = [numPages]time.Duration{5 * time.Second, 2 * time.Second, 5 * time.Second}

func main() {
	snapshot := flag.String("png", "", "render one frame to this PNG file and exit (no framebuffer needed)")
	pageFlag := flag.Int("page", 0, "page for -png: 0 status, 1 resources, 2 containers")
	flag.IntVar(&batOffsetMV, "bat-offset-mv", 100, "subtract this from the fuel gauge voltage (the JZ08C's VM-BMS reads ~100 mV high)")
	flag.Parse()
	log.SetFlags(0)

	mr, wr := newModemReader(), newWifiReader()
	cs := &cpuSampler{}
	cs.cpuPct() // prime the CPU counters

	frameFor := func(page int) *image.RGBA {
		s := collect(mr, wr)
		var r Res
		var ct []Container
		switch page {
		case 1:
			r = readRes(cs)
		case 2:
			ct = containers()
		}
		return renderPage(page, s, r, ct)
	}

	if *snapshot != "" {
		f, err := os.Create(*snapshot)
		if err != nil {
			log.Fatal(err)
		}
		defer f.Close()
		if *pageFlag == 1 {
			time.Sleep(500 * time.Millisecond) // give the CPU sampler a window
		}
		if err := png.Encode(f, frameFor(*pageFlag%numPages)); err != nil {
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
	screenOn, page := true, 0
	setBacklight(true)

	keys := make(chan keyEvent, 16)
	for _, f := range openButtons() {
		go watchButton(f, keys)
	}

	draw := func() {
		toXRGB(frameFor(page), frame)
		if _, err := fb.WriteAt(frame, 0); err != nil {
			log.Printf("framebuffer: %v", err)
		}
	}
	setScreen := func(on bool) {
		screenOn = on
		setBacklight(on)
		if on {
			draw()
		} else {
			_, _ = fb.WriteAt(blank, 0) // black too, in case the backlight module is missing
		}
		log.Printf("screen on=%v", on)
	}

	refresh := time.NewTimer(pageInterval[page])
	hold := time.NewTimer(time.Hour)
	hold.Stop()
	pressed, consumed := false, false
	draw()
	for {
		select {
		case <-refresh.C:
			if screenOn {
				draw()
			}
			refresh.Reset(pageInterval[page])

		case ev := <-keys:
			switch {
			case ev.down && !pressed:
				pressed, consumed = true, false
				hold.Reset(holdToggle)
			case !ev.down && pressed:
				pressed = false
				hold.Stop()
				if consumed {
					break // long press already handled
				}
				if !screenOn { // a short press wakes the screen
					setScreen(true)
					break
				}
				page = (page + 1) % numPages
				draw()
				refresh.Reset(pageInterval[page])
			}

		case <-hold.C: // held for holdToggle: toggle the screen right away
			if pressed {
				consumed = true
				setScreen(!screenOn)
			}
		}
	}
}
