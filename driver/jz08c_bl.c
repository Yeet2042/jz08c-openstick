// SPDX-License-Identifier: GPL-2.0
/*
 * Backlight on/off for the JZ08C pocket WiFi TFT.
 *
 * The panel backlight is driven by PM8916 MPP4 in current-sink mode
 * (set up by the "lcd-bl-state" pinctrl state in the device tree).
 * The pinctrl/gpio API can only flip the mode bit, which does not gate
 * the sink. What actually turns it off is the MPP master enable
 * (EN_CTL bit 7), so this driver writes that register directly and
 * exposes it as /sys/class/backlight/jz08c-bl (brightness 0 or 1).
 *
 * Note: anything that reconfigures MPP4 through pinctrl/gpio afterwards
 * (e.g. writing the "lcd-backlight" LED) rewrites EN_CTL from the pinctrl
 * driver's cache and turns the sink back on.
 */
#include <linux/backlight.h>
#include <linux/module.h>
#include <linux/of.h>
#include <linux/of_platform.h>
#include <linux/platform_device.h>
#include <linux/regmap.h>

#define MPP_STRIDE		0x100
#define MPP_REG_EN_CTL		0x46
#define MPP_EN			BIT(7)

static unsigned int mpp = 4;
module_param(mpp, uint, 0444);
MODULE_PARM_DESC(mpp, "PM8916 MPP number driving the backlight (1-4)");

static struct platform_device *mpp_pdev;
static struct backlight_device *bl;
static struct regmap *map;
static u32 base;

static int jz08c_bl_update(struct backlight_device *bd)
{
	bool on = backlight_get_brightness(bd) > 0;

	return regmap_update_bits(map, base + MPP_REG_EN_CTL, MPP_EN, on ? MPP_EN : 0);
}

static const struct backlight_ops jz08c_bl_ops = {
	.update_status = jz08c_bl_update,
};

static int __init jz08c_bl_init(void)
{
	struct backlight_properties props = {
		.type = BACKLIGHT_RAW,
		.max_brightness = 1,
		.brightness = 1,
	};
	struct device_node *np;
	unsigned int val;
	u32 mpp_base;
	int ret;

	if (mpp < 1 || mpp > 4)
		return -EINVAL;

	np = of_find_compatible_node(NULL, NULL, "qcom,pm8916-mpp");
	if (!np)
		return -ENODEV;
	ret = of_property_read_u32(np, "reg", &mpp_base);
	mpp_pdev = of_find_device_by_node(np);
	of_node_put(np);
	if (ret || !mpp_pdev)
		return -ENODEV;

	/* the SPMI PMIC (parent of the MPP block) owns the regmap */
	map = dev_get_regmap(mpp_pdev->dev.parent, NULL);
	if (!map) {
		ret = -EPROBE_DEFER;
		goto err_put;
	}
	base = mpp_base + (mpp - 1) * MPP_STRIDE;

	ret = regmap_read(map, base + MPP_REG_EN_CTL, &val);
	if (ret)
		goto err_put;
	props.brightness = !!(val & MPP_EN);

	bl = backlight_device_register("jz08c-bl", &mpp_pdev->dev, NULL,
				       &jz08c_bl_ops, &props);
	if (IS_ERR(bl)) {
		ret = PTR_ERR(bl);
		goto err_put;
	}
	pr_info("jz08c-bl: MPP%u at 0x%x, backlight %s\n", mpp, base,
		props.brightness ? "on" : "off");
	return 0;

err_put:
	put_device(&mpp_pdev->dev);
	return ret;
}

static void __exit jz08c_bl_exit(void)
{
	/* leave the backlight on when unloading */
	regmap_update_bits(map, base + MPP_REG_EN_CTL, MPP_EN, MPP_EN);
	backlight_device_unregister(bl);
	put_device(&mpp_pdev->dev);
}

module_init(jz08c_bl_init);
module_exit(jz08c_bl_exit);

MODULE_DESCRIPTION("JZ08C TFT backlight via PM8916 MPP master enable");
MODULE_LICENSE("GPL");
