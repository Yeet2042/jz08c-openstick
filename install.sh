#!/bin/sh
# Install the JZ08C extras on a board that already runs OpenStick Debian
# (flashed with the v40 recipe, see README). Run on the board as root:
#
#   sudo ./install.sh [--kernel jz08c-kernel-6.12.1.tar.gz] [--ecm]
#
#   --kernel FILE  install kernel 6.12 (+ modules, dtb, backlight module) from the
#                  release tarball and boot it; without it the stock OpenStick 6.6
#                  kernel is kept and the 6.6 dtb/module from this repo are used
#   --ecm          switch the USB gadget from RNDIS to CDC-ECM (works on macOS)
#
# Needs internet on the board for apt.
set -eu
cd "$(dirname "$0")"
[ "$(id -u)" = 0 ] || { echo "run as root" >&2; exit 1; }

KERNEL=""
ECM=0
while [ $# -gt 0 ]; do
    case "$1" in
        --kernel) KERNEL=$(readlink -f "$2"); shift 2 ;;
        --ecm) ECM=1; shift ;;
        *) echo "unknown option $1" >&2; exit 1 ;;
    esac
done

EXT=/boot/extlinux/extlinux.conf
CMDLINE=$(sed -n 's/^append //p' "$EXT")

echo "==> packages"
apt-get install -y -qq --no-install-recommends iw >/dev/null

echo "==> WiFi: keep the Debian 12 wpa_supplicant"
# wpasupplicant 2:2.10-24 (Debian 13) can't start the AP on wcn36xx
# ("hal_delete_sta_self ... err=7"); 2:2.10-12+deb12u3 works.
if ! dpkg -l wpasupplicant 2>/dev/null | grep -q 'deb12'; then
    echo "deb http://deb.debian.org/debian bookworm main" > /etc/apt/sources.list.d/bookworm.list
    printf 'Package: *\nPin: release n=bookworm\nPin-Priority: 100\n' > /etc/apt/preferences.d/bookworm
    apt-get update -qq
    V=$(apt-cache policy wpasupplicant | awk '/deb12/{print $1; exit}' | sed 's/^\*\*\*//')
    [ -n "$V" ] && apt-get install -y -qq --allow-downgrades "wpasupplicant=$V" >/dev/null
fi
apt-mark hold wpasupplicant >/dev/null

[ -f "$EXT.orig" ] || cp "$EXT" "$EXT.orig"
if [ -n "$KERNEL" ]; then
    echo "==> kernel 6.12 from $KERNEL"
    tar -xzf "$KERNEL" -C /
    KREL=$(tar -tzf "$KERNEL" | sed -n 's#^\./lib/modules/\([^/]*\)/$#\1#p' | head -1)
    printf 'linux /vmlinuz-6.12\nfdt /dtbs/qcom/msm8916-jz08c-v50.dtb\nappend %s\n' "$CMDLINE" > "$EXT"
else
    echo "==> device tree for the stock 6.6 kernel"
    KREL=$(uname -r)
    install -m644 dts/msm8916-jz08c.dtb /boot/dtbs/qcom/msm8916-jz08c.dtb
    sed -i 's#^fdt .*#fdt /dtbs/qcom/msm8916-jz08c.dtb#' "$EXT"
    if [ "$KREL" = "6.6.0-msm8916" ]; then
        install -D -m644 driver/jz08c_bl-6.6.0-msm8916.ko "/lib/modules/$KREL/extra/jz08c_bl.ko"
        depmod -a "$KREL"
    else
        echo "   kernel $KREL: build jz08c_bl from driver/ (skipped)"
    fi
fi
cat "$EXT"

echo "==> TFT panel firmware and modules to load"
install -m644 firmware/jz08c-st7735s.bin /lib/firmware/jz08c-st7735s.bin
echo panel-mipi-dbi > /etc/modules-load.d/jz08c-panel.conf
echo jz08c_bl > /etc/modules-load.d/jz08c-bl.conf

echo "==> status screen"
if [ -x status/jz-status-arm64 ]; then
    install -m755 status/jz-status-arm64 /usr/local/bin/jz-status
    install -m644 board/jz-status-go.service /etc/systemd/system/jz-status.service
else
    echo "   status/jz-status-arm64 not found: using the Python version"
    apt-get install -y -qq --no-install-recommends python3 python3-pil fonts-dejavu-core >/dev/null
    install -m755 board/jz-status.py /usr/local/bin/jz-status.py
    install -m644 board/jz-status.service /etc/systemd/system/jz-status.service
fi

echo "==> low-battery shutdown, power key"
install -m755 board/jz-lowbat /usr/local/sbin/jz-lowbat
install -m644 board/jz-lowbat.service board/jz-lowbat.timer /etc/systemd/system/
install -D -m644 board/logind-powerkey.conf /etc/systemd/logind.conf.d/jz08c-powerkey.conf

if [ "$ECM" = 1 ]; then
    echo "==> USB gadget: CDC-ECM"
    install -m755 board/usb-ecm-gadget /usr/local/sbin/usb-ecm-gadget
    install -D -m644 board/usb-gadget-ecm.conf /etc/systemd/system/usb-gadget.service.d/ecm.conf
    nmcli con mod usb ipv4.method auto ipv4.addresses 192.168.6.1/24 ipv4.route-metric 50 \
        ipv4.dhcp-timeout infinity ipv4.may-fail yes connection.autoconnect-retries 0 ipv6.method disabled
fi

# The hotspot can fail once at boot (supplicant timeout); keep retrying.
nmcli con mod hotspot connection.autoconnect-retries 0 2>/dev/null || true
# NetworkManager runs its own dnsmasq; the system one only fights it for port 53.
systemctl disable --now dnsmasq.service 2>/dev/null || true

systemctl daemon-reload
systemctl enable jz-status.service jz-lowbat.timer >/dev/null

echo
echo "Done. Reboot: sudo reboot"
echo "Original boot config saved as $EXT.orig."
