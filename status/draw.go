package main

import (
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"os"
	"strings"
	"time"

	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/gofont/gomono"
	"golang.org/x/image/font/gofont/gomonobold"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
	"golang.org/x/image/vector"
)

const W, H = 128, 128

var (
	colBG     = color.RGBA{10, 14, 22, 255}
	colFG     = color.RGBA{230, 235, 240, 255}
	colDim    = color.RGBA{120, 130, 145, 255}
	colAccent = color.RGBA{64, 160, 255, 255}
	colGood   = color.RGBA{70, 200, 110, 255}
	colWarn   = color.RGBA{240, 180, 50, 255}
	colBad    = color.RGBA{230, 70, 70, 255}
	colHeader = color.RGBA{24, 34, 52, 255}
	colLine   = color.RGBA{40, 48, 62, 255}
	colOffBar = color.RGBA{50, 58, 72, 255}
)

var fSmall, fBold, fBig font.Face

func mustFace(ttf []byte, size float64) font.Face {
	f, err := opentype.Parse(ttf)
	if err != nil {
		panic(err)
	}
	face, err := opentype.NewFace(f, &opentype.FaceOptions{Size: size, DPI: 72, Hinting: font.HintingFull})
	if err != nil {
		panic(err)
	}
	return face
}

func init() {
	fSmall = mustFace(gomono.TTF, 10)
	fBold = mustFace(gomonobold.TTF, 10)
	fBig = mustFace(gobold.TTF, 15)
}

type canvas struct{ *image.RGBA }

func (c canvas) rect(x0, y0, x1, y1 int, col color.Color) {
	draw.Draw(c, image.Rect(x0, y0, x1+1, y1+1), image.NewUniform(col), image.Point{}, draw.Src)
}

func (c canvas) outline(x0, y0, x1, y1 int, col color.Color) {
	c.rect(x0, y0, x1, y0, col)
	c.rect(x0, y1, x1, y1, col)
	c.rect(x0, y0, x0, y1, col)
	c.rect(x1, y0, x1, y1, col)
}

func textWidth(face font.Face, s string) int {
	return font.MeasureString(face, s).Ceil()
}

// text draws s with its top-left corner at (x, y).
func (c canvas) text(x, y int, s string, face font.Face, col color.Color) {
	d := font.Drawer{Dst: c, Src: image.NewUniform(col), Face: face}
	d.Dot = fixed.P(x, y+face.Metrics().Ascent.Ceil())
	d.DrawString(s)
}

func (c canvas) textRight(xr, y int, s string, face font.Face, col color.Color) {
	c.text(xr-textWidth(face, s), y, s, face, col)
}

func (c canvas) polygon(col color.Color, pts ...[2]float32) {
	r := vector.NewRasterizer(W, H)
	r.MoveTo(pts[0][0], pts[0][1])
	for _, p := range pts[1:] {
		r.LineTo(p[0], p[1])
	}
	r.ClosePath()
	r.Draw(c, c.Bounds(), image.NewUniform(col), image.Point{})
}

func levelColor(pct, good, warn int) color.RGBA {
	switch {
	case pct >= good:
		return colGood
	case pct >= warn:
		return colWarn
	}
	return colBad
}

// battery icon, 16x8, with a lightning mark on USB power
func (c canvas) battery(x, y, pct int, usb bool) {
	c.outline(x, y, x+14, y+8, colFG)
	c.rect(x+15, y+2, x+16, y+6, colFG)
	if fill := (12*pct + 50) / 100; fill > 0 {
		c.rect(x+1, y+1, x+fill, y+7, levelColor(pct, 50, 20))
	}
	if usb {
		fx, fy := float32(x), float32(y)
		c.polygon(color.White,
			[2]float32{fx + 8, fy + 1}, [2]float32{fx + 4, fy + 5}, [2]float32{fx + 7, fy + 5},
			[2]float32{fx + 6, fy + 8}, [2]float32{fx + 10, fy + 3}, [2]float32{fx + 7, fy + 3})
	}
}

// five signal bars
func (c canvas) bars(x, y, pct int, col color.Color) {
	n := 0
	if pct > 0 {
		n = 1 + pct/20
		if n > 5 {
			n = 5
		}
	}
	for i := 0; i < 5; i++ {
		h := 4 + i*3
		bc := color.Color(colOffBar)
		if i < n {
			bc = col
		}
		c.rect(x+i*6, y+16-h, x+i*6+4, y+16, bc)
	}
}

type Status struct {
	Host    string
	Battery int
	USB     bool
	Modem   Modem
	IPs     [3]string // usb0, wlan0, wwan0
	Clients int
}

func render(s Status) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, W, H))
	c := canvas{img}
	c.rect(0, 0, W-1, H-1, colBG)

	// header: hostname, battery, time
	c.rect(0, 0, W-1, 14, colHeader)
	c.text(3, 2, strings.ToUpper(strings.TrimPrefix(s.Host, "openstick-")), fBold, colAccent)
	c.textRight(W-3, 2, time.Now().Format("15:04"), fBold, colFG)
	if s.Battery >= 0 {
		c.battery(34, 3, s.Battery, s.USB)
		c.text(54, 2, fmt.Sprintf("%d%%", s.Battery), fSmall, colFG)
	}

	// signal block
	m := s.Modem
	y := 20
	switch {
	case !m.Present:
		c.text(4, y+2, "No modem", fBig, colBad)
	case m.State == mmStateFailed && m.Failed == mmFailedSIM:
		c.bars(4, y, 0, colBad)
		c.text(40, y+1, "No SIM", fBig, colBad)
	default:
		c.bars(4, y, m.Signal, levelColor(m.Signal, 50, 25))
		c.text(40, y+1, fmt.Sprintf("%d%%", m.Signal), fBig, colFG)
		c.textRight(W-4, y+4, m.Tech, fBold, colAccent)
		op := m.Operator
		if op == "" {
			op = m.StateName()
		}
		if len(op) > 20 {
			op = op[:20]
		}
		c.text(4, y+21, op, fSmall, colDim)
	}

	// IP addresses
	y = 58
	c.rect(4, y-4, W-5, y-4, colLine)
	for i, label := range []string{"USB", "WiFi", "4G"} {
		c.text(4, y, label, fBold, colDim)
		c.text(34, y, s.IPs[i], fSmall, colFG)
		y += 14
	}

	// WiFi clients
	y += 4
	c.rect(4, y-4, W-5, y-4, colLine)
	c.text(4, y, "Clients", fBold, colDim)
	cc := color.RGBA(colFG)
	if s.Clients > 0 {
		cc = colGood
	}
	c.text(60, y, fmt.Sprint(s.Clients), fBold, cc)
	return img
}

// toXRGB converts to the panel's 32-bit little-endian XRGB8888 (bytes B,G,R,X).
func toXRGB(img *image.RGBA, out []byte) {
	p := img.Pix
	for i := 0; i+3 < len(p); i += 4 {
		out[i], out[i+1], out[i+2], out[i+3] = p[i+2], p[i+1], p[i], 0
	}
}

func hostname() string {
	h, err := os.Hostname()
	if err != nil {
		return "jz08"
	}
	return h
}
