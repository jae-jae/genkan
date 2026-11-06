package plugin

import (
	"errors"
	"math"
	"strconv"
	"strings"
)

func sshNumber(s string) (float64, error) {
	n, err := strconv.ParseFloat(s, 64)
	if err != nil || math.IsNaN(n) || math.IsInf(n, 0) || n < 0 || n > 9007199254740991 {
		return 0, errors.New("invalid counter")
	}
	return n, nil
}

func sshCPU(s string) (total, idle float64, err error) {
	f := strings.Fields(s)
	if len(f) < 9 || f[0] != "cpu" {
		return 0, 0, errors.New("invalid cpu")
	}
	// Guest and guest_nice are already included in user/nice; do not count twice.
	for i := 1; i <= 8; i++ {
		n, e := sshNumber(f[i])
		if e != nil {
			return 0, 0, e
		}
		total += n
		if i == 4 || i == 5 {
			idle += n
		}
	}
	return
}

func sshNetwork(s string) (map[string][2]float64, error) {
	out := map[string][2]float64{}
	for _, line := range strings.Split(s, "\n") {
		name, rest, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		name = strings.TrimSpace(name)
		if name == "lo" {
			continue
		}
		f := strings.Fields(rest)
		if len(f) != 16 {
			return nil, errors.New("invalid network")
		}
		rx, e1 := sshNumber(f[0])
		tx, e2 := sshNumber(f[8])
		if e1 != nil || e2 != nil {
			return nil, errors.New("invalid network")
		}
		out[name] = [2]float64{rx, tx}
	}
	return out, nil
}

func parseSSHStats(raw string) (map[string]any, error) {
	fail := errors.New("invalid Linux statistics")
	sections := map[string]string{}
	key := ""
	for _, line := range strings.Split(raw, "\n") {
		if strings.HasPrefix(line, "@gk:") {
			key = strings.TrimPrefix(line, "@gk:")
			if _, exists := sections[key]; exists {
				return nil, fail
			}
			sections[key] = ""
		} else if key != "" {
			sections[key] += line + "\n"
		}
	}
	for _, k := range []string{"host", "kernel", "arch", "cores", "cpu1", "cpu2", "time1", "time2", "memory", "load", "disk", "net1", "net2"} {
		sections[k] = strings.TrimSpace(sections[k])
		if sections[k] == "" {
			return nil, fail
		}
	}
	for _, k := range []string{"host", "kernel", "arch"} {
		if len(sections[k]) > 255 || strings.Contains(sections[k], "\n") {
			return nil, fail
		}
	}
	cores, e := sshNumber(sections["cores"])
	if e != nil || cores < 1 || cores != math.Trunc(cores) {
		return nil, fail
	}
	t1, i1, e1 := sshCPU(sections["cpu1"])
	t2, i2, e2 := sshCPU(sections["cpu2"])
	if e1 != nil || e2 != nil || t2 <= t1 {
		return nil, fail
	}
	cpu := math.Max(0, math.Min(100, 100*(1-(i2-i1)/(t2-t1))))
	u1, e1 := sshNumber(strings.Fields(sections["time1"])[0])
	u2, e2 := sshNumber(strings.Fields(sections["time2"])[0])
	if e1 != nil || e2 != nil || u2 <= u1 || u2-u1 > 30 {
		return nil, fail
	}
	memory := map[string]float64{}
	for _, line := range strings.Split(sections["memory"], "\n") {
		f := strings.Fields(line)
		if len(f) < 2 {
			continue
		}
		name := strings.TrimSuffix(f[0], ":")
		if name != "MemTotal" && name != "MemAvailable" && name != "SwapTotal" && name != "SwapFree" {
			continue
		}
		n, e := sshNumber(f[1])
		if e != nil || len(f) != 3 || f[2] != "kB" {
			return nil, fail
		}
		memory[name] = n * 1024
	}
	total, ok := memory["MemTotal"]
	available, ok2 := memory["MemAvailable"]
	if !ok || !ok2 || total <= 0 || available > total {
		return nil, fail
	}
	loads := strings.Fields(sections["load"])
	if len(loads) < 3 {
		return nil, fail
	}
	load := []float64{}
	for _, v := range loads[:3] {
		n, e := sshNumber(v)
		if e != nil {
			return nil, fail
		}
		load = append(load, n)
	}
	disk := strings.Fields(sections["disk"])
	if len(disk) < 6 || disk[len(disk)-1] != "/" {
		return nil, fail
	}
	dt, e1 := sshNumber(disk[1])
	du, e2 := sshNumber(disk[2])
	da, e3 := sshNumber(disk[3])
	if e1 != nil || e2 != nil || e3 != nil || dt <= 0 || du > dt || da > dt || du+da <= 0 {
		return nil, fail
	}
	net1, e1 := sshNetwork(sections["net1"])
	net2, e2 := sshNetwork(sections["net2"])
	if e1 != nil || e2 != nil {
		return nil, fail
	}
	var rx, tx float64
	for name, counters := range net2 {
		previous, exists := net1[name]
		if !exists {
			continue
		} // New/reset interfaces have no valid rate this interval.
		if counters[0] >= previous[0] {
			rx += counters[0] - previous[0]
		}
		if counters[1] >= previous[1] {
			tx += counters[1] - previous[1]
		}
	}
	return map[string]any{
		"hostname": sections["host"], "kernel": sections["kernel"], "arch": sections["arch"],
		"cores": cores, "cpuPercent": cpu, "uptimeSeconds": u2, "load": load,
		"memory":  map[string]any{"totalBytes": total, "usedBytes": total - available, "percent": 100 * (total - available) / total},
		"disk":    map[string]any{"mount": "/", "totalBytes": dt * 1024, "usedBytes": du * 1024, "percent": 100 * du / (du + da)},
		"network": map[string]any{"rxBytesPerSecond": rx / (u2 - u1), "txBytesPerSecond": tx / (u2 - u1)},
	}, nil
}
