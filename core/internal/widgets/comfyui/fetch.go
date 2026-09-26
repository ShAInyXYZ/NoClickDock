package comfyui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/ShAInyXYZ/NoClickDock/core/internal/config"
	"github.com/ShAInyXYZ/NoClickDock/core/internal/sensors"
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

// Ask "probe" (host, port): what Settings' Test button reports — the address
// actually asked, and what answered there, before anything is saved.
func (w *Widget) Ask(ctx context.Context, what string, args map[string]string) (string, []string, error) {
	if what != "probe" {
		return "", nil, fmt.Errorf("ComfyUI cannot answer %q", what)
	}
	host := strings.TrimSpace(args["host"])
	port, err := strconv.Atoi(strings.TrimSpace(args["port"]))
	if !config.ValidHost(host) {
		return "", nil, fmt.Errorf("%q is not a host name or an address", host)
	}
	if err != nil || port < 1 || port > 65535 {
		return "", nil, fmt.Errorf("%q is not a port (1–65535)", args["port"])
	}
	base := baseURL(host, port)
	q, err := fetchQueue(ctx, w.client, base)
	if err != nil {
		return "", nil, fmt.Errorf("%s did not answer: %s", base, why(err, "/queue"))
	}
	sys, devices, err := fetchStats(ctx, w.client, base)
	if err != nil {
		return "", nil, fmt.Errorf("%s answered /queue but not /system_stats: %s", base, why(err, "/system_stats"))
	}
	var names []string
	for _, d := range devices {
		names = append(names, sensors.ShortGPUName(d.Name))
	}
	parts := []string{base + " answered"}
	if sys.ComfyUI != "" {
		parts = append(parts, "ComfyUI "+sys.ComfyUI)
	}
	switch len(devices) {
	case 0:
		parts = append(parts, "no devices")
	case 1:
		parts = append(parts, names[0])
	default:
		parts = append(parts, fmt.Sprintf("%d devices (%s)", len(devices), strings.Join(names, ", ")))
	}
	switch {
	case q.Running > 0:
		parts = append(parts, fmt.Sprintf("generating, %d pending", q.Pending))
	case q.Pending > 0:
		parts = append(parts, fmt.Sprintf("%d pending", q.Pending))
	default:
		parts = append(parts, "queue empty")
	}
	return strings.Join(parts, " · "), nil, nil
}

// why says a failed request in a few words.
func why(err error, path string) string {
	msg := err.Error()
	switch {
	case errors.Is(err, context.DeadlineExceeded) || strings.Contains(msg, "Client.Timeout") || strings.Contains(msg, "i/o timeout"):
		return fmt.Sprintf("no answer within %s", fetchTimeout)
	case strings.Contains(msg, "connection refused"):
		return "connection refused (nothing listens on that port)"
	case strings.Contains(msg, "no such host"):
		return "no such host"
	case strings.Contains(msg, "no route to host"), strings.Contains(msg, "network is unreachable"):
		return "the host cannot be reached"
	case strings.Contains(msg, ": 4"), strings.Contains(msg, ": 5"):
		if i := strings.LastIndex(msg, ": "); i >= 0 {
			return "HTTP " + msg[i+2:] + " on " + path + " — is it ComfyUI?"
		}
	}
	var syntax *json.SyntaxError
	var typeErr *json.UnmarshalTypeError
	if errors.As(err, &syntax) || errors.As(err, &typeErr) {
		return "the answer on " + path + " is not ComfyUI's"
	}
	return msg
}
