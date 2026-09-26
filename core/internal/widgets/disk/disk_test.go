package disk

import (
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/ShAInyXYZ/NoClickDock/core/internal/model"
)

func TestParseMountsKeepsWhatCanFillUp(t *testing.T) {
	f, err := os.Open("testdata/proc-mounts.txt")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var got []string
	for _, m := range parseMounts(f) {
		got = append(got, [...]string{"local", "pool", "network"}[m.kind]+" "+m.point+" "+m.fstype)
	}
	want := []string{
		"local / ext4",
		"local /boot/efi vfat",
		"local /home ext4",
		"local /mnt/Media Library xfs", // \040 decoded
		"local /snapshots btrfs",       // a sibling of /snap, not under it
		"pool /srv/media zfs",
		"network /mnt/nas nfs4",
		"network /mnt/share cifs",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("\n got %q\nwant %q", got, want)
	}
}

func TestParseSizeIsDecimalLikeDocker(t *testing.T) {
	for in, want := range map[string]uint64{
		"83.54GB": 83_540_000_000,
		"1.2kB":   1_200,
		"0B":      0,
		"512B":    512,
		"1.5TB":   1_500_000_000_000,
		"12 MB":   12_000_000,
		"N/A":     0,
		"":        0,
	} {
		if got := parseSize(in); got != want {
			t.Errorf("parseSize(%q) = %d, want %d", in, got, want)
		}
	}
}

func TestParseDockerDF(t *testing.T) {
	out, _ := os.ReadFile("testdata/docker-df.txt")
	d := parseDockerDF(string(out))
	if len(d.rows) != 4 || d.rows[3].kind != "Build Cache" {
		t.Fatalf("rows = %+v", d.rows)
	}
	if d.total != 68_130_000_000 {
		t.Fatalf("total = %d", d.total)
	}
}

const gib = 1 << 30

func fs(mount string, total, used, free uint64) Filesystem {
	return Filesystem{Mount: mount, Label: mount, FSType: "ext4", Total: total * gib, Used: used * gib, Free: free * gib}
}

func TestStateFollowsTheFullestFilesystem(t *testing.T) {
	share := fs("/mnt/nas", 1000, 930, 70)
	share.Kind, share.Device = Network, "//nas.example/share"
	silent := Filesystem{Mount: "/mnt/offsite", Label: "/mnt/offsite", FSType: "nfs4", Kind: Network, Unresponsive: true}
	cases := []struct {
		name   string
		fss    []Filesystem
		state  model.State
		label  string
		glance model.Glance
	}{
		{"healthy: the tightest free space", []Filesystem{fs("/", 500, 300, 175), fs("/home", 2000, 1000, 900)},
			model.StateOK, "175G free", model.Glance{Text: "60%"}},
		{"84.9% is still fine", []Filesystem{fs("/", 1000, 849, 100)}, model.StateOK, "100G free", model.Glance{Text: "85%"}},
		{"85% warns", []Filesystem{fs("/", 500, 300, 175), fs("/data", 100, 85, 10)},
			model.StateWarn, "/data 85% full", model.Glance{Text: "85%", Tone: model.ToneWarn}},
		{"92% is critical", []Filesystem{fs("/", 100, 92, 3)},
			model.StateCrit, "/ 92% full", model.Glance{Text: "92%", Tone: model.ToneCrit}},
		{"a full share drives the dial", []Filesystem{fs("/", 500, 300, 175), share},
			model.StateCrit, "/mnt/nas 93% full", model.Glance{Text: "93%", Tone: model.ToneCrit}},
		{"a silent share is a warning", []Filesystem{fs("/", 500, 300, 175), silent},
			model.StateWarn, "/mnt/offsite not responding", model.Glance{Text: "60%", Tone: model.ToneWarn}},
		{"nothing mounted", nil, model.StateIdle, "no filesystems", model.Glance{}},
		{"zero-size filesystems are ignored", []Filesystem{{Mount: "/empty"}}, model.StateIdle, "no filesystems", model.Glance{}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := build(c.fss, nil, nil)
			if s.State != c.state || s.StateLabel != c.label || s.Glance != c.glance {
				t.Fatalf("got %s %q %+v, want %s %q %+v", s.State, s.StateLabel, s.Glance, c.state, c.label, c.glance)
			}
		})
	}
}

func TestPanelSpellsOutEveryFilesystemBySection(t *testing.T) {
	pool := fs("/srv/media", 8000, 7040, 560)
	pool.Kind, pool.FSType, pool.Label, pool.Device = Pool, "zfs", "tank/media", "tank/media"
	silent := Filesystem{Mount: "/mnt/offsite", Label: "/mnt/offsite", FSType: "nfs4", Kind: Network, Unresponsive: true}
	s := build([]Filesystem{fs("/", 500, 300, 175), fs("/data", 100, 93, 2), pool, silent}, nil, nil)
	blocks := s.Pages[0].Blocks

	var kinds []string
	for _, b := range blocks {
		kinds = append(kinds, b.Kind())
	}
	if want := []string{"section", "meter", "meter", "section", "meter", "section", "row"}; !reflect.DeepEqual(kinds, want) {
		t.Fatalf("blocks %v, want %v", kinds, want)
	}
	if sec := blocks[0].(model.Section); sec.Title != "Filesystems" || sec.Meta != "2 · warn 85 · crit 92" {
		t.Fatalf("section = %+v", sec)
	}
	root := blocks[1].(model.Meter)
	if root.Label != "/" || !root.Mono || root.Value != "175G free" || root.Note != "300G used of 500G · 60% · ext4" || root.Tone != model.ToneOK {
		t.Fatalf("root meter = %+v", root)
	}
	if data := blocks[2].(model.Meter); data.Tone != model.ToneCrit || data.Frac != 0.93 {
		t.Fatalf("data meter = %+v", data)
	}
	if sec := blocks[3].(model.Section); sec.Title != "Pools" || sec.Meta != "1" {
		t.Fatalf("pools = %+v", sec)
	}
	if m := blocks[4].(model.Meter); m.Label != "tank/media" || m.Note != "6.9T used of 7.8T · 88% · mounted at /srv/media" {
		t.Fatalf("pool meter = %+v", m)
	}
	if row := blocks[6].(model.Row); row.Name != "/mnt/offsite" || row.Value != "not responding" || row.Tone != model.ToneWarn {
		t.Fatalf("silent row = %+v", row)
	}
	want := []model.Issue{
		{ID: "/data", Text: "/data: 93% full · 2.0G left", Tone: model.ToneCrit},
		{ID: "/srv/media", Text: "tank/media: 88% full · 560G left", Tone: model.ToneWarn},
		{ID: "noresp:/mnt/offsite", Text: "/mnt/offsite is not responding", Tone: model.ToneWarn},
	}
	if !reflect.DeepEqual(s.Issues, want) {
		t.Fatalf("issues = %+v", s.Issues)
	}
}

func TestDockerSection(t *testing.T) {
	out, _ := os.ReadFile("testdata/docker-df.txt")
	d := parseDockerDF(string(out))
	d.root = "/var/lib/docker"
	s := build([]Filesystem{fs("/", 500, 300, 175)}, d, nil)
	blocks := s.Pages[0].Blocks[2:]
	if sec := blocks[0].(model.Section); sec.Title != "Docker" || sec.Meta != "reclaimable" {
		t.Fatalf("section = %+v", sec)
	}
	if row := blocks[1].(model.Row); row.Name != "63G could be freed" || row.Value != "/var/lib/docker" || row.Tone != model.ToneWarn {
		t.Fatalf("headline = %+v", row)
	}
	stats := blocks[2].(model.Stats)
	if stats.Items[0] != (model.Stat{Key: "images", Value: "36G", Tone: model.ToneWarn}) ||
		stats.Items[1] != (model.Stat{Key: "containers", Value: "1.3G"}) {
		t.Fatalf("stats = %+v", stats.Items)
	}
	if hint := blocks[3].(model.Note); !hint.Mono || !strings.HasPrefix(hint.Text, "docker system prune -a") {
		t.Fatalf("hint = %+v", hint)
	}

	// Little to reclaim: said quietly, no prune hint.
	small := &dockerSpace{total: 3e9, root: "/", rows: []dockerRow{{"Images", 3e9}}}
	s = build([]Filesystem{fs("/", 500, 300, 175)}, small, nil)
	if n := len(s.Pages[0].Blocks); n != 5 {
		t.Fatalf("got %d blocks, want filesystems + section, headline, stats", n)
	}
	if row := s.Pages[0].Blocks[3].(model.Row); row.Tone != model.ToneNone {
		t.Fatalf("a small reclaim is toned %q", row.Tone)
	}
}

func TestProbeGivesUpAndDoesNotPileUp(t *testing.T) {
	var p prober
	release := make(chan struct{})
	var calls atomic.Int32
	stuck := func() (int, error) {
		calls.Add(1)
		<-release
		return 1, nil
	}
	if _, _, answered := probe(&p, "/mnt/offsite", 10*time.Millisecond, stuck); answered {
		t.Fatal("a stuck share answered")
	}
	// Still stuck from the last poll: not asked again.
	if _, _, answered := probe(&p, "/mnt/offsite", 10*time.Millisecond, stuck); answered || calls.Load() != 1 {
		t.Fatalf("asked %d times", calls.Load())
	}
	close(release)
	deadline := time.Now().Add(time.Second)
	for {
		v, err, answered := probe(&p, "/mnt/offsite", 100*time.Millisecond, func() (int, error) { return 7, nil })
		if answered {
			if v != 7 || err != nil {
				t.Fatalf("got %d, %v", v, err)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the share never recovered")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestMockIsInventedAndWithinTheContract(t *testing.T) {
	s := New().Mock(time.Time{})
	if s.State != model.StateWarn || utf8.RuneCountInString(s.Glance.Text) > 5 {
		t.Fatalf("mock state %s glance %q", s.State, s.Glance.Text)
	}
	raw, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	// Invented mounts only: never a device path from a real machine.
	for _, real := range []string{"nvme", "/dev/"} {
		if strings.Contains(string(raw), real) {
			t.Errorf("mock mentions %q", real)
		}
	}
}

func TestSortPutsRootFirst(t *testing.T) {
	fss := []Filesystem{{Mount: "/srv"}, {Mount: "/"}, {Mount: "/home"}}
	sortFilesystems(fss)
	if fss[0].Mount != "/" || fss[1].Mount != "/home" || fss[2].Mount != "/srv" {
		t.Fatalf("order = %+v", fss)
	}
}
