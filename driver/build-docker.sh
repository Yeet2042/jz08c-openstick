#!/bin/sh
# Build jz08c_bl.ko for the OpenStick kernel (6.6.0-msm8916) in an arm64 Debian
# container. Works on Apple Silicon Macs (OrbStack / Docker Desktop) and arm64 Linux.
#
#   ./build-docker.sh <kernel-config>
#
# Get the config from the board:  ssh user@<board> 'zcat /proc/config.gz' > config
# The kernel has no MODVERSIONS or module signing, so a Module.symvers-less
# build (KBUILD_MODPOST_WARN=1) loads fine as long as vermagic matches.
set -eu
cd "$(dirname "$0")"
CONFIG=$(cd "$(dirname "${1:?usage: $0 <kernel-config>}")" && pwd)/$(basename "$1")
SRC=linux-6.6.tar.xz
SHA=d926a06c63dd8ac7df3f86ee1ffc2ce2a3b81a2d168484e76b5b389aba8e56d0

[ -f "$SRC" ] || curl -fL -o "$SRC" "https://cdn.kernel.org/pub/linux/kernel/v6.x/$SRC"
echo "$SHA  $SRC" | shasum -a 256 -c -

docker run --rm --platform linux/arm64 -v "$PWD":/w -v "$CONFIG":/config:ro \
    -v jz08c-ksrc:/src debian:bookworm bash -ec '
apt-get update -qq >/dev/null
apt-get install -y -qq --no-install-recommends build-essential bc flex bison libssl-dev libelf-dev xz-utils kmod >/dev/null
if [ ! -x /src/linux-6.6/scripts/mod/modpost ]; then
    tar -xJf /w/linux-6.6.tar.xz -C /src
    cd /src/linux-6.6 && cp /config .config && make -s olddefconfig >/dev/null && make -s -j"$(nproc)" modules_prepare
fi
rm -rf /mod && mkdir /mod && cp /w/jz08c_bl.c /w/Makefile /mod/
make -s -C /src/linux-6.6 M=/mod KBUILD_MODPOST_WARN=1 modules 2>&1 | grep -v "undefined!" || true
cp /mod/jz08c_bl.ko /w/jz08c_bl-6.6.0-msm8916.ko
modinfo /w/jz08c_bl-6.6.0-msm8916.ko | grep vermagic
'
