#!/usr/bin/env python3
"""Status screen for the JZ08C 128x128 TFT (/dev/fb0, XRGB8888).

Shows the hostname, the time, 4G signal and operator, IP addresses and the
number of WiFi clients. Redraws every few seconds.
"""
import os
import select
import socket
import struct
import subprocess
import time

from PIL import Image, ImageDraw, ImageFont

FB = "/dev/fb0"
W = H = 128
INTERVAL = 5

FONT_DIR = "/usr/share/fonts/truetype/dejavu"
F_SMALL = ImageFont.truetype(f"{FONT_DIR}/DejaVuSansMono.ttf", 10)
F_BOLD = ImageFont.truetype(f"{FONT_DIR}/DejaVuSansMono-Bold.ttf", 10)
F_BIG = ImageFont.truetype(f"{FONT_DIR}/DejaVuSans-Bold.ttf", 15)

BG = (10, 14, 22)
FG = (230, 235, 240)
DIM = (120, 130, 145)
ACCENT = (64, 160, 255)
GOOD = (70, 200, 110)
WARN = (240, 180, 50)
BAD = (230, 70, 70)
HEADER = (24, 34, 52)


def run(cmd):
    try:
        return subprocess.run(cmd, capture_output=True, text=True, timeout=4).stdout
    except Exception:
        return ""


def modem_info():
    kv = {}
    for line in run(["mmcli", "-m", "any", "--output-keyvalue"]).splitlines():
        if ":" in line:
            k, v = line.split(":", 1)
            kv[k.strip()] = v.strip()
    if not kv:
        return {"state": "no modem"}
    tech = kv.get("modem.generic.access-technologies.value[1]", "--")
    return {
        "state": kv.get("modem.generic.state", "--"),
        "failed": kv.get("modem.generic.state-failed-reason", "--"),
        "signal": int(kv.get("modem.generic.signal-quality.value", "0") or 0),
        "tech": {"lte": "4G", "umts": "3G", "hspa": "3G", "gsm": "2G"}.get(tech, tech),
        "operator": kv.get("modem.3gpp.operator-name", "--"),
    }


def ipv4(dev):
    addrs = []
    for line in run(["ip", "-4", "-o", "addr", "show", "dev", dev]).splitlines():
        f = line.split()
        if "inet" in f:
            addrs.append(f[f.index("inet") + 1].split("/")[0])
    # usb0 also carries the fixed fallback address; prefer the DHCP one
    addrs = [a for a in addrs if not a.startswith("192.168.6.")] or addrs
    return addrs[0] if addrs else "-"


def wifi_clients():
    out = run(["iw", "dev", "wlan0", "station", "dump"])
    return sum(1 for line in out.splitlines() if line.startswith("Station"))


def read(path, default=""):
    try:
        with open(path) as f:
            return f.read().strip()
    except OSError:
        return default


def battery():
    cap = read("/sys/class/power_supply/pm8916-bms-vm/capacity")
    usb = read("/sys/class/power_supply/pm8916-lbc-chgr/online") == "1"
    return (int(cap) if cap.isdigit() else None), usb


def draw_battery(d, x, y, pct, usb):
    """Small battery icon (16x8) with fill level; a lightning mark when on USB power."""
    color = GOOD if pct >= 50 else WARN if pct >= 20 else BAD
    d.rectangle([x, y, x + 14, y + 8], outline=FG)
    d.rectangle([x + 15, y + 2, x + 16, y + 6], fill=FG)
    fill = round(12 * pct / 100)
    if fill:
        d.rectangle([x + 1, y + 1, x + fill, y + 7], fill=color)
    if usb:
        d.polygon([(x + 8, y + 1), (x + 4, y + 5), (x + 7, y + 5), (x + 6, y + 8), (x + 10, y + 3), (x + 7, y + 3)],
                  fill=(255, 255, 255))


def draw_bars(d, x, y, pct, color):
    n = 0 if pct <= 0 else min(5, 1 + pct // 20)
    for i in range(5):
        h = 4 + i * 3
        box = [x + i * 6, y + 16 - h, x + i * 6 + 4, y + 16]
        d.rectangle(box, fill=color if i < n else (50, 58, 72))


def render():
    img = Image.new("RGB", (W, H), BG)
    d = ImageDraw.Draw(img)

    # header: hostname + time
    d.rectangle([0, 0, W, 14], fill=HEADER)
    d.text((3, 2), socket.gethostname().replace("openstick-", "").upper(), font=F_BOLD, fill=ACCENT)
    clock = time.strftime("%H:%M")
    d.text((W - 3 - d.textlength(clock, font=F_BOLD), 2), clock, font=F_BOLD, fill=FG)

    # battery in the middle of the header
    pct, usb = battery()
    if pct is not None:
        draw_battery(d, 34, 3, pct, usb)
        d.text((54, 2), f"{pct}%", font=F_SMALL, fill=FG)

    # signal block
    m = modem_info()
    y = 20
    if m["state"] == "no modem":
        d.text((4, y + 2), "No modem", font=F_BIG, fill=BAD)
    elif m.get("failed") == "sim-missing":
        draw_bars(d, 4, y, 0, BAD)
        d.text((40, y + 1), "No SIM", font=F_BIG, fill=BAD)
    else:
        pct = m["signal"]
        color = GOOD if pct >= 50 else WARN if pct >= 25 else BAD
        draw_bars(d, 4, y, pct, color)
        d.text((40, y + 1), f"{pct}%", font=F_BIG, fill=FG)
        d.text((W - 4 - d.textlength(m["tech"], font=F_BOLD), y + 4), m["tech"], font=F_BOLD, fill=ACCENT)
        op = m["operator"] if m["operator"] != "--" else m["state"]
        d.text((4, y + 21), op[:20], font=F_SMALL, fill=DIM)

    # IP addresses
    y = 58
    d.line([4, y - 4, W - 4, y - 4], fill=(40, 48, 62))
    for label, dev in (("USB", "usb0"), ("WiFi", "wlan0"), ("4G", "wwan0")):
        d.text((4, y), label, font=F_BOLD, fill=DIM)
        d.text((34, y), ipv4(dev), font=F_SMALL, fill=FG)
        y += 14

    # WiFi clients
    y += 4
    d.line([4, y - 4, W - 4, y - 4], fill=(40, 48, 62))
    n = wifi_clients()
    d.text((4, y), "Clients", font=F_BOLD, fill=DIM)
    d.text((60, y), str(n), font=F_BOLD, fill=GOOD if n else FG)
    return img


# provided by the jz08c_bl module (PM8916 MPP4 master enable); the "lcd-backlight"
# LED must not be written, it would re-enable the sink from the pinctrl cache
BACKLIGHT = "/sys/class/backlight/jz08c-bl/brightness"
BUTTONS = ("pm8941_pwrkey", "GPIO Buttons")   # short press on either toggles the screen
EV_KEY = 1
EVENT_SIZE = struct.calcsize("llHHi")          # struct input_event on 64-bit


def open_buttons():
    """Open the evdev nodes of the board's buttons (looked up by name)."""
    fds, name = [], None
    for line in read("/proc/bus/input/devices").splitlines():
        if line.startswith("N: Name="):
            name = line.split("=", 1)[1].strip('"')
        elif line.startswith("H: Handlers=") and name in BUTTONS:
            ev = next((h for h in line.split("=", 1)[1].split() if h.startswith("event")), None)
            if ev:
                fds.append(os.open(f"/dev/input/{ev}", os.O_RDONLY | os.O_NONBLOCK))
    return fds


def pressed(fd):
    """True if the pending events on fd include a key press (value 1)."""
    hit = False
    try:
        while True:
            buf = os.read(fd, EVENT_SIZE * 16)
            if not buf:
                break
            for i in range(0, len(buf) - EVENT_SIZE + 1, EVENT_SIZE):
                _, _, etype, _, value = struct.unpack_from("llHHi", buf, i)
                if etype == EV_KEY and value == 1:
                    hit = True
    except BlockingIOError:
        pass
    return hit


def set_backlight(on):
    try:
        with open(BACKLIGHT, "w") as f:
            f.write("1" if on else "0")
    except OSError as e:
        print("backlight:", e, flush=True)


LEDS = "/sys/class/leds"
LEDS_ENABLED = False      # indicator LEDs are too bright; keep them all off
_led_cache = {}


def set_led(name, value):
    """Write an LED attribute only when it changes."""
    if _led_cache.get(name) == value:
        return
    try:
        attr = "trigger" if not value.isdigit() else "brightness"
        with open(f"{LEDS}/{name}/{attr}", "w") as f:
            f.write(value)
        _led_cache[name] = value
    except OSError:
        pass


def update_leds():
    """Indicator lamps: green = 4G data up, yellow = searching / no data, red = no SIM or low battery."""
    m = modem_info()
    pct, usb = battery()
    low_bat = pct is not None and pct <= 15 and not usb
    if m["state"] == "no modem" or m.get("failed") not in (None, "--") or low_bat:
        red, green = 1, 0
    elif m["state"] == "connected" and ipv4("wwan0") != "-":
        red, green = 0, 1
    else:
        red, green = 1, 1
    set_led("red:status", str(red))
    set_led("green:status", str(green))


def main():
    if LEDS_ENABLED:
        set_led("green:aux", "phy0tx")      # middle green LEDs blink on WiFi traffic
    else:
        for name in ("red:status", "green:status", "green:aux"):
            set_led(name, "none")
            set_led(name, "0")
    buttons = open_buttons()
    print("watching buttons:", [os.readlink(f"/proc/self/fd/{fd}") for fd in buttons], flush=True)
    screen_on = True
    set_backlight(True)
    with open(FB, "r+b", buffering=0) as fb:
        while True:
            try:
                if LEDS_ENABLED:
                    update_leds()
            except Exception as e:
                print("leds:", e, flush=True)
            if screen_on:
                try:
                    data = render().convert("RGBX").tobytes("raw", "BGRX")
                    fb.seek(0)
                    fb.write(data)
                except Exception as e:  # keep running if one refresh fails
                    print("refresh failed:", e, flush=True)
            ready, _, _ = select.select(buttons, [], [], INTERVAL)
            if any([pressed(fd) for fd in ready]):   # drain every ready fd
                screen_on = not screen_on
                set_backlight(screen_on)
                if not screen_on:
                    # black frame too, in case the backlight module isn't loaded
                    fb.seek(0)
                    fb.write(bytes(W * H * 4))
                print("button: screen", "on" if screen_on else "off",
                      "backlight=" + read(BACKLIGHT, "?"), flush=True)


if __name__ == "__main__":
    os.nice(5)
    main()
