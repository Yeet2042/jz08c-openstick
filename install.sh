#!/bin/sh
# Install the JZ08C extras on a board that already runs OpenStick Debian
# (flashed with the v40 recipe, see README). Run on the board as root:
#
#   sudo ./install.sh            # dtb, TFT, battery, backlight, buttons, low-battery shutdown
#   sudo ./install.sh --ecm      # also switch the USB gadget from RNDIS to CDC-ECM (macOS)
#
# Needs internet on the board for apt (python3-pil, fonts, iw).
set -eu
cd "$(dirname "$0")"
[ "$(id -u)" = 0 ] || { echo "run as root" >&2; exit 1; }

KREL=$(uname -r)
DTB=/boot/dtbs/qcom/msm8916-jz08c.dtb
EXT=/boot/extlinux/extlinux.conf

echo "==> packages"
apt-get install -y -qq --no-install-recommends python3 python3-pil fonts-dejavu-core iw >/dev/null

echo "==> device tree"
install -m644 dts/msm8916-jz08c.dtb "$DTB"
[ -f "$EXT.orig" ] || cp "$EXT" "$EXT.orig"
sed -i 's#^fdt .*#fdt /dtbs/qcom/msm8916-jz08c.dtb#' "$EXT"
grep '^fdt' "$EXT"

echo "==> TFT panel firmware"
install -m644 firmware/jz08c-st7735s.bin /lib/firmware/jz08c-st7735s.bin
echo panel-mipi-dbi > /etc/modules-load.d/jz08c-panel.conf

echo "==> backlight driver"
if [ "$KREL" = "6.6.0-msm8916" ]; then
    install -D -m644 driver/jz08c_bl-6.6.0-msm8916.ko "/lib/modules/$KREL/extra/jz08c_bl.ko"
    depmod -a
    echo jz08c_bl > /etc/modules-load.d/jz08c-bl.conf
else
    echo "   kernel $KREL: prebuilt module is for 6.6.0-msm8916, build it from driver/ (skipped)"
fi

echo "==> status screen, buttons, low-battery shutdown"
install -m755 board/jz-status.py /usr/local/bin/jz-status.py
install -m644 board/jz-status.service /etc/systemd/system/jz-status.service
install -m755 board/jz-lowbat /usr/local/sbin/jz-lowbat
install -m644 board/jz-lowbat.service board/jz-lowbat.timer /etc/systemd/system/
install -D -m644 board/logind-powerkey.conf /etc/systemd/logind.conf.d/jz08c-powerkey.conf

if [ "${1:-}" = "--ecm" ]; then
    echo "==> USB gadget: CDC-ECM"
    install -m755 board/usb-ecm-gadget /usr/local/sbin/usb-ecm-gadget
    install -D -m644 board/usb-gadget-ecm.conf /etc/systemd/system/usb-gadget.service.d/ecm.conf
    nmcli con mod usb ipv4.method auto ipv4.addresses 192.168.6.1/24 ipv4.route-metric 50 \
        ipv4.dhcp-timeout infinity ipv4.may-fail yes connection.autoconnect-retries 0 ipv6.method disabled
fi

systemctl daemon-reload
systemctl enable jz-status.service jz-lowbat.timer >/dev/null

echo
echo "Done. Reboot to load the new device tree: sudo reboot"
echo "If it does not come back, reflash the OpenStick boot image over fastboot (see README)."
