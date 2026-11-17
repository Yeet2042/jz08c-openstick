#!/bin/sh
# Build kernel 6.12.1-msm8916 + the JZ08C dtb + modules (and jz08c_bl) in an arm64
# Debian container. Output: kernel/jz08c-kernel-6.12.1.tar.gz (/boot + /lib/modules).
set -eu
cd "$(dirname "$0")/.."
SRC=kernel/linux-6.12.1-msm8916.tar.gz
[ -s "$SRC" ] || gh api repos/msm8916-mainline/linux/tarball/v6.12.1-msm8916 > "$SRC" \
    || curl -fL -o "$SRC" https://codeload.github.com/msm8916-mainline/linux/tar.gz/refs/tags/v6.12.1-msm8916
docker run --rm --platform linux/arm64 -v "$PWD":/w -v jz08c-k612:/src debian:bookworm bash /w/kernel/build-in-container.sh
