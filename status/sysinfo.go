package main

import (
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

// Res is the data for the resources page.
type Res struct {
	CPUPct    int     // busy % since the previous sample
	FreqMHz   int     // average current CPU frequency
	TempC     float64 // hottest CPU thermal zone
	Load      [3]string
	MemUsedMB int
	MemTotMB  int
	SwapUsed  int // MB
	SwapTot   int // MB
	ZramRatio float64
	DiskUsed  float64 // GiB
	DiskTot   float64 // GiB
	Uptime    string
}

type cpuSampler struct{ idle, total uint64 }

// cpuPct returns CPU busy % since the last call (aggregate "cpu" line of /proc/stat).
func (s *cpuSampler) cpuPct() int {
	line := strings.SplitN(readFile("/proc/stat"), "\n", 2)[0]
	f := strings.Fields(line)
	if len(f) < 5 {
		return 0
	}
	var total, idle uint64
	for i, v := range f[1:] {
		n, _ := strconv.ParseUint(v, 10, 64)
		total += n
		if i == 3 || i == 4 { // idle + iowait
			idle += n
		}
	}
	dt, di := total-s.total, idle-s.idle
	s.total, s.idle = total, idle
	if dt == 0 {
		return 0
	}
	return int(100 * (dt - di) / dt)
}

func meminfo() map[string]int {
	m := map[string]int{}
	for _, l := range strings.Split(readFile("/proc/meminfo"), "\n") {
		f := strings.Fields(l)
		if len(f) >= 2 {
			v, _ := strconv.Atoi(f[1])
			m[strings.TrimSuffix(f[0], ":")] = v // kB
		}
	}
	return m
}

func avgFreqMHz() int {
	files, _ := filepath.Glob("/sys/devices/system/cpu/cpu[0-9]*/cpufreq/scaling_cur_freq")
	sum, n := 0, 0
	for _, p := range files {
		if v, err := strconv.Atoi(readFile(p)); err == nil {
			sum += v
			n++
		}
	}
	if n == 0 {
		return 0
	}
	return sum / n / 1000
}

func cpuTemp() float64 {
	zones, _ := filepath.Glob("/sys/class/thermal/thermal_zone*")
	max := 0
	for _, z := range zones {
		if !strings.HasPrefix(readFile(z+"/type"), "cpu") {
			continue
		}
		if v, err := strconv.Atoi(readFile(z + "/temp")); err == nil && v > max {
			max = v
		}
	}
	return float64(max) / 1000
}

func uptime() string {
	f := strings.Fields(readFile("/proc/uptime"))
	if len(f) == 0 {
		return "-"
	}
	sec, _ := strconv.ParseFloat(f[0], 64)
	s := int(sec)
	d, h, m := s/86400, s%86400/3600, s%3600/60
	if d > 0 {
		return strconv.Itoa(d) + "d " + strconv.Itoa(h) + "h"
	}
	return strconv.Itoa(h) + "h " + strconv.Itoa(m) + "m"
}

func readRes(cs *cpuSampler) Res {
	r := Res{CPUPct: cs.cpuPct(), FreqMHz: avgFreqMHz(), TempC: cpuTemp(), Uptime: uptime()}
	if f := strings.Fields(readFile("/proc/loadavg")); len(f) >= 3 {
		r.Load = [3]string{f[0], f[1], f[2]}
	}
	m := meminfo()
	r.MemTotMB = m["MemTotal"] / 1024
	r.MemUsedMB = (m["MemTotal"] - m["MemAvailable"]) / 1024
	r.SwapTot = m["SwapTotal"] / 1024
	r.SwapUsed = (m["SwapTotal"] - m["SwapFree"]) / 1024
	// /sys/block/zram0/mm_stat: orig_data_size compr_data_size ...
	if f := strings.Fields(readFile("/sys/block/zram0/mm_stat")); len(f) >= 2 {
		orig, _ := strconv.ParseFloat(f[0], 64)
		comp, _ := strconv.ParseFloat(f[1], 64)
		if comp > 0 {
			r.ZramRatio = orig / comp
		}
	}
	var st syscall.Statfs_t
	if syscall.Statfs("/", &st) == nil {
		bs := float64(st.Bsize)
		r.DiskTot = float64(st.Blocks) * bs / (1 << 30) // GiB, like df -h
		r.DiskUsed = float64(st.Blocks-st.Bfree) * bs / (1 << 30)
	}
	return r
}
