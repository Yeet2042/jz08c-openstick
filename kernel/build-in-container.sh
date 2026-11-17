#!/bin/bash
# runs inside debian:bookworm arm64; /w = work/k612 (Mac), /src = docker volume (case-sensitive)
set -e
apt-get update -qq >/dev/null
apt-get install -y -qq --no-install-recommends build-essential bc flex bison libssl-dev libelf-dev xz-utils kmod cpio python3 rsync >/dev/null
K=/src/linux-6.12
if [ ! -f $K/Makefile ]; then
  mkdir -p $K && tar -xzf /w/kernel/linux-6.12.1-msm8916.tar.gz -C $K --strip-components=1
fi
cd $K
cp /w/dts/msm8916-jz08c-v50.dts arch/arm64/boot/dts/qcom/
grep -q msm8916-jz08c-v50 arch/arm64/boot/dts/qcom/Makefile || \
  sed -i '/msm8916-thwc-ufi001c.dtb/a dtb-$(CONFIG_ARCH_QCOM)\t+= msm8916-jz08c-v50.dtb' arch/arm64/boot/dts/qcom/Makefile
cp /w/kernel/config-6.6-base .config
scripts/config --set-str LOCALVERSION "-msm8916" \
  -m DRM_PANEL_MIPI_DBI -m CHARGER_PM8916_LBC -m BATTERY_PM8916_BMS_VM \
  -e LEDS_GPIO -e BACKLIGHT_CLASS_DEVICE -d LOCALVERSION_AUTO \
  -e ZRAM_BACKEND_ZSTD -e ZRAM_BACKEND_LZ4 \
  -m NFT_FIB_INET -m NFT_FIB_IPV4 -m NFT_FIB_IPV6 -m NFT_REDIR -m NFT_NUMGEN \
  -m NFT_HASH -m NFT_QUOTA -m NFT_CONNLIMIT -m NFT_SOCKET
make -s olddefconfig
echo "kernelrelease: $(make -s kernelrelease)"
make -s -j$(nproc) Image.gz modules qcom/msm8916-jz08c-v50.dtb 2>&1 | grep -E "error|Error" || true
rm -rf /out && mkdir -p /out/boot/dtbs/qcom
cp arch/arm64/boot/Image.gz /out/boot/vmlinuz-6.12
cp arch/arm64/boot/dts/qcom/msm8916-jz08c-v50.dtb /out/boot/dtbs/qcom/
# modules go to usr/lib/modules: on merged-/usr Debian, /lib is a symlink and a
# "./lib/" entry in the tarball would make tar replace it with a real directory
make -s INSTALL_MOD_PATH=/out/usr INSTALL_MOD_STRIP=1 modules_install
# out-of-tree backlight module for this kernel
rm -rf /mod && mkdir /mod && cp /w/driver/jz08c_bl.c /w/driver/Makefile /mod/
make -s -C $K M=/mod modules
KR=$(make -s kernelrelease)
install -D -m644 /mod/jz08c_bl.ko /out/usr/lib/modules/$KR/extra/jz08c_bl.ko
depmod -b /out/usr $KR
cp .config /w/kernel/config-6.12
tar -C /out -czf /w/kernel/jz08c-kernel-6.12.1.tar.gz .
ls -l /w/kernel/jz08c-kernel-6.12.1.tar.gz /out/boot/vmlinuz-6.12 /out/boot/dtbs/qcom/
echo BUILD-OK $KR
