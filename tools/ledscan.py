#!/usr/bin/env python3
"""Find which TLMM GPIOs drive the JZ08C indicator LEDs.

Drives each unused GPIO high, then low, while the TFT shows the pin number,
so a person watching the board can note which number lights an LED.
Each pin is returned to input afterwards.
"""
import subprocess
import sys
import time

from PIL import Image, ImageDraw, ImageFont

PINS = [int(p) for p in sys.argv[1].split(",")]
STEP = float(sys.argv[2]) if len(sys.argv) > 2 else 0.8
LOG = "/tmp/ledscan.log"
F_BIG = ImageFont.truetype("/usr/share/fonts/truetype/dejavu/DejaVuSans-Bold.ttf", 40)
F_SM = ImageFont.truetype("/usr/share/fonts/truetype/dejavu/DejaVuSans-Bold.ttf", 16)


def show(pin, level, idx):
    im = Image.new("RGB", (128, 128), (0, 0, 0))
    d = ImageDraw.Draw(im)
    d.text((64, 20), "GPIO", font=F_SM, fill=(150, 150, 150), anchor="mm")
    d.text((64, 60), str(pin), font=F_BIG, fill=(255, 255, 255), anchor="mm")
    d.text((64, 100), level, font=F_SM, fill=(80, 200, 255) if level == "HIGH" else (255, 170, 60), anchor="mm")
    d.text((64, 120), f"{idx}/{len(PINS)}", font=ImageFont.load_default(), fill=(90, 90, 90), anchor="mm")
    with open("/dev/fb0", "wb") as fb:
        fb.write(im.convert("RGBX").tobytes("raw", "BGRX"))


def drive(pin, val, seconds):
    p = subprocess.Popen(["gpioset", "-c", "gpiochip0", f"{pin}={val}"],
                         stdout=subprocess.DEVNULL, stderr=subprocess.PIPE)
    time.sleep(seconds)
    alive = p.poll() is None
    p.terminate()
    p.wait()
    return alive


for n in range(15, 0, -1):            # give the person time to look at the LEDs
    im = Image.new("RGB", (128, 128), (0, 0, 0)); d = ImageDraw.Draw(im)
    d.text((64, 30), "LED scan in", font=F_SM, fill=(200, 200, 200), anchor="mm")
    d.text((64, 75), str(n), font=F_BIG, fill=(255, 220, 0), anchor="mm")
    open("/dev/fb0", "wb").write(im.convert("RGBX").tobytes("raw", "BGRX"))
    time.sleep(1)

with open(LOG, "w") as log:
    for i, pin in enumerate(PINS, 1):
        for val, name in ((1, "HIGH"), (0, "LOW")):
            show(pin, name, i)
            ok = drive(pin, val, STEP)
            print(f"{time.strftime('%T')} gpio{pin} {name} {'ok' if ok else 'FAILED'}", file=log, flush=True)
        # back to input
        subprocess.run(["gpioget", "-c", "gpiochip0", str(pin)], capture_output=True)
    print("done", file=log, flush=True)
