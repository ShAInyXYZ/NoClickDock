package power

import (
	"encoding/hex"
	"encoding/json"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/ShAInyXYZ/NoClickDock/core/internal/sensors"
)

// display is one monitor lit by the desktop: its port, its mode and, from
// its own EDID, what it is and roughly what it draws.
type display struct {
	Port, Res string // "DP-0", "2560x1440"
	Maker     string // PNP id, "DEL"
	Model     string // the EDID's name descriptor, "U2723QE"
	Inches    float64
	EstW      float64 // 0: unknown
}

type edid struct {
	maker, model string
	inches, estW float64
}

// parseEDID reads a base EDID block: the PNP manufacturer id (bytes 8–9),
// the monitor name descriptor (0xFC), and the image size in cm (bytes
// 21–22) → inches → an estimate: round((inches·1.1 + 2)·1.5) W. That fit
// assumes a plain SDR panel and under-reads 4K/HDR ones; the 1.5× keeps it
// honest rather than flattering. Still an estimate, shown as one.
func parseEDID(b []byte) (edid, bool) {
	if len(b) < 128 || string(b[:8]) != "\x00\xff\xff\xff\xff\xff\xff\x00" {
		return edid{}, false
	}
	id := uint16(b[8])<<8 | uint16(b[9])
	letter := func(v uint16) byte { return byte(v&0x1f) + 64 }
	e := edid{maker: string([]byte{letter(id >> 10), letter(id >> 5), letter(id)})}
	for i := 54; i+18 <= 126; i += 18 {
		if b[i] == 0 && b[i+1] == 0 && b[i+2] == 0 && b[i+3] == 0xfc {
			name := b[i+5 : i+18]
			if j := strings.IndexByte(string(name), '\n'); j >= 0 {
				name = name[:j]
			}
			e.model = strings.TrimSpace(strings.Map(func(r rune) rune {
				if r < 0x20 || r > 0x7e {
					return -1
				}
				return r
			}, string(name)))
			break
		}
	}
	if h, v := float64(b[21]), float64(b[22]); h > 0 && v > 0 {
		e.inches = math.Sqrt(h*h+v*v) / 2.54
		e.estW = math.RoundToEven((e.inches*1.1 + 2) * 1.5)
	}
	return e, true
}

var (
	outputRE = regexp.MustCompile(`^(\S+) connected\b(.*)$`)
	modeRE   = regexp.MustCompile(`\b(\d+x\d+)\+\d+\+\d+\b`)
	hexRE    = regexp.MustCompile(`^[0-9a-fA-F]+$`)
)

// parseXrandr reads `xrandr --verbose --prop`: every connected output with
// an active mode (a BMC's virtual display has none), each with the EDID
// printed under it — so a monitor is paired with its own EDID, never by
// order (the Python widget paired sysfs EDIDs with xrandr outputs by
// order, and the two orders differ).
func parseXrandr(out string) []display {
	var displays []display
	var cur *display
	var hexBuf strings.Builder
	inEDID := false
	flush := func() {
		if cur != nil && hexBuf.Len() > 0 {
			if raw, err := hex.DecodeString(hexBuf.String()); err == nil {
				if e, ok := parseEDID(raw); ok {
					cur.Maker, cur.Model, cur.Inches, cur.EstW = e.maker, e.model, e.inches, e.estW
				}
			}
		}
		hexBuf.Reset()
		inEDID = false
	}
	for _, line := range strings.Split(out, "\n") {
		if line == "" {
			continue
		}
		if line[0] != ' ' && line[0] != '\t' {
			flush()
			cur = nil
			m := outputRE.FindStringSubmatch(line)
			if m == nil {
				continue
			}
			if mode := modeRE.FindStringSubmatch(m[2]); mode != nil {
				displays = append(displays, display{Port: m[1], Res: mode[1]})
				cur = &displays[len(displays)-1]
			}
			continue
		}
		t := strings.TrimSpace(line)
		switch {
		case cur == nil:
		case strings.HasPrefix(t, "EDID:"):
			flush()
			inEDID = true
		case inEDID && hexRE.MatchString(t):
			hexBuf.WriteString(t)
		case inEDID:
			flush()
		}
	}
	flush()
	return displays
}

var lsusbRE = regexp.MustCompile(`^Bus \d+ Device \d+: ID [0-9a-fA-F]{4}:[0-9a-fA-F]{4} (.+)$`)

// parseLsusb: the peripherals, hubs and readers left out.
func parseLsusb(out string) []string {
	var names []string
	for _, line := range strings.Split(out, "\n") {
		m := lsusbRE.FindStringSubmatch(strings.TrimSpace(line))
		if m == nil {
			continue
		}
		name := strings.TrimSpace(m[1])
		lower := strings.ToLower(name)
		skip := false
		for _, s := range []string{"root hub", "hub", "virtual", "smart card reader"} {
			skip = skip || strings.Contains(lower, s)
		}
		if !skip && name != "" {
			names = append(names, name)
		}
	}
	return names
}

// parseDmidecodeMemory: "4x 32 GB · DDR5 · 4800 MT/s · Kingston".
func parseDmidecodeMemory(out string) string {
	count := 0
	var size, kind, speed, maker string
	for _, line := range strings.Split(out, "\n") {
		t := strings.TrimSpace(line)
		key, value, ok := strings.Cut(t, ":")
		if !ok {
			continue
		}
		value = strings.TrimSpace(value)
		switch key {
		case "Size":
			if !strings.Contains(value, "No Module") && value != "" {
				count++
				size = value
			}
		case "Type":
			if kind == "" && value != "" && !strings.Contains(value, "Unknown") && !strings.Contains(value, "Error") {
				kind = value
			}
		case "Configured Memory Speed":
			if speed == "" && value != "" && value != "Unknown" {
				speed = value
			}
		case "Manufacturer":
			if maker == "" && value != "" && value != "Unknown" && value != "Not Specified" {
				maker = value
			}
		}
	}
	var parts []string
	if count > 0 && size != "" {
		parts = append(parts, strconv.Itoa(count)+"x "+size)
	}
	for _, p := range []string{kind, speed, maker} {
		if p != "" {
			parts = append(parts, p)
		}
	}
	return strings.Join(parts, " · ")
}

// parseBaseboard: dmidecode -t baseboard's Product Name.
func parseBaseboard(out string) string {
	for _, line := range strings.Split(out, "\n") {
		if key, value, ok := strings.Cut(strings.TrimSpace(line), ":"); ok && key == "Product Name" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

// parseGDDR6 reads gddr6-oneshot: "11:00.0 62" per card it supports (other
// lines, like "Device: …", are skipped).
func parseGDDR6(out string) map[string]float64 {
	temps := map[string]float64{}
	for _, line := range strings.Split(out, "\n") {
		f := strings.Fields(line)
		if len(f) != 2 || !strings.Contains(f[0], ":") {
			continue
		}
		if v, err := strconv.Atoi(f[1]); err == nil {
			temps[strings.ToLower(f[0])] = float64(v)
		}
	}
	return temps
}

// parseRocmSMI reads `rocm-smi --showpower --showtemp --showmeminfo vram
// --json`, card by card in index order. Load and fan are not in it.
func parseRocmSMI(out string) []sensors.GPU {
	var cards map[string]map[string]any
	if json.Unmarshal([]byte(out), &cards) != nil {
		return nil
	}
	keys := make([]string, 0, len(cards))
	for k := range cards {
		if strings.HasPrefix(k, "card") {
			keys = append(keys, k)
		}
	}
	sort.Slice(keys, func(i, j int) bool {
		a, _ := strconv.Atoi(strings.TrimPrefix(keys[i], "card"))
		b, _ := strconv.Atoi(strings.TrimPrefix(keys[j], "card"))
		return a < b
	})
	num := func(v any) *float64 {
		var f float64
		switch x := v.(type) {
		case float64:
			f = x
		case string:
			var err error
			if f, err = strconv.ParseFloat(strings.TrimSpace(x), 64); err != nil {
				return nil
			}
		default:
			return nil
		}
		return &f
	}
	mib := func(v any) *float64 {
		if f := num(v); f != nil {
			m := *f / 1048576
			return &m
		}
		return nil
	}
	var gpus []sensors.GPU
	for i, k := range keys {
		c := cards[k]
		name, _ := c["Card series"].(string)
		if name == "" {
			name = "AMD GPU"
		}
		gpus = append(gpus, sensors.GPU{
			Index: i, Name: name,
			Power: num(c["Average Graphics Package Power (W)"]), Limit: num(c["Max Graphics Package Power (W)"]),
			Temp:     num(c["Temperature (Sensor edge) (C)"]),
			VRAMUsed: mib(c["VRAM Total Used Memory (B)"]), VRAMTotal: mib(c["VRAM Total Memory (B)"]),
		})
	}
	return gpus
}

// rapl turns successive energy counters (µJ) into watts, across a wrap.
type rapl struct {
	prev, max uint64
	at        float64 // seconds
	primed    bool
	last      *float64
}

// next is the draw since the previous reading; one too soon after it (a
// refresh right after a poll) repeats the last value rather than measure
// over a few milliseconds.
func (r *rapl) next(energy uint64, at float64) *float64 {
	if r.primed && at-r.at <= 0.2 {
		return r.last
	}
	prev, prevAt, primed := r.prev, r.at, r.primed
	r.prev, r.at, r.primed = energy, at, true
	dt := at - prevAt
	if !primed {
		return nil
	}
	var delta uint64
	switch {
	case energy >= prev:
		delta = energy - prev
	case r.max > 0 && prev <= r.max:
		delta = r.max - prev + energy // the counter wrapped
	default:
		r.last = nil
		return nil
	}
	w := float64(delta) / 1e6 / dt
	r.last = &w
	return &w
}
