#!/usr/bin/env python3
"""Convert a Qualcomm mdss-spi-on-command blob into a panel-mipi-dbi firmware file.
Stock format per entry: <delay_ms_after> <len> <cmd> <params...>
"""
import sys
on = bytes.fromhex(sys.argv[1]); out = sys.argv[2]
SKIP = {0x2a, 0x2b, 0x2c}          # CASET/RASET/RAMWR: the driver sets the window itself
fw = bytearray(b"MIPI DBI" + bytes(7) + bytes([1]))
i = 0
while i < len(on):
    delay, ln = on[i], on[i+1]; cmd = on[i+2]; params = on[i+3:i+2+ln]; i += 2 + ln
    print(f"cmd {cmd:02x} params {params.hex(' ') or '-'} delay {delay}")
    if cmd in SKIP: continue
    fw += bytes([cmd, len(params)]) + params
    if delay: fw += bytes([0x00, 1, delay])     # command 0x00 = delay in ms
open(out, "wb").write(fw)
print(len(fw), "bytes ->", out)
