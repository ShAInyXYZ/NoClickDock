package comfyui

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	userAgent    = "noclickdock/2 (+comfyui)"
	fetchTimeout = 5 * time.Second
)

// baseURL is the endpoint's address; an IPv6 host is bracketed (the Python
// widget built "http://::1:8188").
func baseURL(host string, port int) string {
	return "http://" + net.JoinHostPort(host, strconv.Itoa(port))
}

type queue struct {
	Running, Pending int
}

type system struct {
	ComfyUI, Torch, Python string
	RAMTotal, RAMFree      float64
}

// rawDevice is one entry of /system_stats' devices, as the server sends it.
type rawDevice struct {
	Name           string  `json:"name"`
	Type           string  `json:"type"`
	Index          *int    `json:"index"`
	VRAMTotal      float64 `json:"vram_total"`
	VRAMFree       float64 `json:"vram_free"`
	TorchVRAMTotal float64 `json:"torch_vram_total"`
	TorchVRAMFree  float64 `json:"torch_vram_free"`
}

// device is what the panel draws for one of them, with the local
// telemetry merged in when the endpoint is this machine.
type device struct {
	Label, Name, Type, Allocator string
	Index                        int
	VRAMTotal, VRAMUsed          float64
	TorchTotal, TorchUsed        float64
	Temp, Util, Power, Fan       *float64
}

func get(ctx context.Context, client *http.Client, url string, into any) error {
	ctx, cancel := context.WithTimeout(ctx, fetchTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", userAgent)
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s: %s", url, resp.Status)
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(into)
}

func fetchQueue(ctx context.Context, client *http.Client, base string) (queue, error) {
	var q struct {
		Running []json.RawMessage `json:"queue_running"`
		Pending []json.RawMessage `json:"queue_pending"`
	}
	if err := get(ctx, client, base+"/queue", &q); err != nil {
		return queue{}, err
	}
	return queue{Running: len(q.Running), Pending: len(q.Pending)}, nil
}

func fetchStats(ctx context.Context, client *http.Client, base string) (system, []device, error) {
	var st struct {
		System struct {
			ComfyUI  string  `json:"comfyui_version"`
			Torch    string  `json:"pytorch_version"`
			Python   string  `json:"python_version"`
			RAMTotal float64 `json:"ram_total"`
			RAMFree  float64 `json:"ram_free"`
		} `json:"system"`
		Devices []rawDevice `json:"devices"`
	}
	if err := get(ctx, client, base+"/system_stats", &st); err != nil {
		return system{}, nil, err
	}
	sys := system{
		ComfyUI: st.System.ComfyUI, RAMTotal: st.System.RAMTotal, RAMFree: st.System.RAMFree,
		// "2.8.0+cu128" → "2.8.0"; "3.12.3 (main, …) [GCC …]" → "3.12.3"
		Torch:  strings.TrimSpace(strings.SplitN(st.System.Torch, "+", 2)[0]),
		Python: strings.TrimSpace(strings.SplitN(st.System.Python, "(", 2)[0]),
	}
	devices := make([]device, 0, len(st.Devices))
	for i, d := range st.Devices {
		devices = append(devices, normalise(d, i))
	}
	return sys, devices, nil
}

// normalise reads "cuda:0 NVIDIA GeForce RTX 4090 : cudaMallocAsync": the
// allocator after the last " : ", the torch device string when the first
// word is one. Everything comes from the response; nothing is assumed about
// the device's kind or count.
func normalise(d rawDevice, position int) device {
	raw := strings.TrimSpace(d.Name)
	name, allocator := raw, ""
	if i := strings.LastIndex(raw, " : "); i >= 0 {
		name, allocator = strings.TrimSpace(raw[:i]), strings.TrimSpace(raw[i+3:])
	}
	label, short := name, name
	if head, tail, ok := strings.Cut(name, " "); ok && strings.Contains(head, ":") && strings.TrimSpace(tail) != "" {
		label, short = head, strings.TrimSpace(tail)
	}
	index := position
	if d.Index != nil {
		index = *d.Index
	}
	if label == "" {
		label = fmt.Sprintf("device %d", index)
	}
	if short == "" {
		short = "—"
	}
	return device{
		Label: label, Name: short, Type: d.Type, Allocator: allocator, Index: index,
		VRAMTotal: d.VRAMTotal, VRAMUsed: max(0, d.VRAMTotal-d.VRAMFree),
		TorchTotal: d.TorchVRAMTotal, TorchUsed: max(0, d.TorchVRAMTotal-d.TorchVRAMFree),
	}
}
