#!/usr/bin/env python3
"""Extract FDT blobs from an Android boot image and dump them as text.
usage: dtbtool.py extract <boot.img> <outdir>
       dtbtool.py dump <file.dtb>
"""
import re, struct, sys, os

def extract(img, out):
    d = open(img, 'rb').read(); i = 0; k = 0
    os.makedirs(out, exist_ok=True)
    while (i := d.find(b'\xd0\x0d\xfe\xed', i)) >= 0:
        sz = struct.unpack('>I', d[i+4:i+8])[0]
        if 1000 < sz < 2_000_000:
            b = d[i:i+sz]; p = f'{out}/dt{k}.dtb'; open(p, 'wb').write(b)
            m = re.search(rb'model\x00', b); print(p, sz); k += 1
        i += 4

def fmt(v):
    if not v: return ''
    if v[-1:] == b'\0' and all(32 <= c < 127 or c == 0 for c in v[:-1]) and v[0] != 0:
        return ' = "' + '", "'.join(x.decode() for x in v[:-1].split(b'\0')) + '"'
    if len(v) % 4 == 0:
        return ' = <' + ' '.join(hex(struct.unpack('>I', v[j:j+4])[0]) for j in range(0, len(v), 4)) + '>'
    return ' = [' + v.hex() + ']'

def dump(p):
    d = open(p, 'rb').read()
    off_s, off_str = struct.unpack('>II', d[8:16]); i = off_s; depth = 0
    while True:
        t = struct.unpack('>I', d[i:i+4])[0]; i += 4
        if t == 1:
            e = d.index(b'\0', i); print('  '*depth + (d[i:e].decode() or '/') + ' {'); depth += 1; i = (e+4) & ~3
        elif t == 2: depth -= 1; print('  '*depth + '};')
        elif t == 3:
            ln, no = struct.unpack('>II', d[i:i+8]); i += 8; v = d[i:i+ln]; i = (i+ln+3) & ~3
            n = d[off_str+no:d.index(b'\0', off_str+no)].decode(); print('  '*depth + n + fmt(v) + ';')
        elif t == 9: break

if __name__ == '__main__':
    {'extract': lambda: extract(sys.argv[2], sys.argv[3]), 'dump': lambda: dump(sys.argv[2])}[sys.argv[1]]()
