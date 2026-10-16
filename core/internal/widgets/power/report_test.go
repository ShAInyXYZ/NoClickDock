package power

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
)

// The report's numbers are the Python generator's: testdata/pmlog is an
// invented 25-day log (a gap, a thin day, rows without a GPU temperature,
// a malformed row), and report_data.golden.json is what
// widgets/power/report/generate_report.py wrote for it under the same
// time-of-use tariff.
func TestReportMatchesThePython(t *testing.T) {
	data, err := buildReport(readRows(logFiles("testdata/pmlog")), tariffTOU20(), "6-13,15-22", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	got := tree(t, data)
	raw, err := os.ReadFile("testdata/report_data.golden.json")
	if err != nil {
		t.Fatal(err)
	}
	var want any
	if err := json.Unmarshal(raw, &want); err != nil {
		t.Fatal(err)
	}
	delete(got.(map[string]any)["period"].(map[string]any), "generated")
	delete(want.(map[string]any)["period"].(map[string]any), "generated")
	var diffs []string
	compare("", got, want, &diffs)
	sort.Strings(diffs)
	for _, d := range diffs[:min(len(diffs), 30)] {
		t.Error(d)
	}
}

// tariffTOU20 is the golden run's config: 0.30 flat, 0.40 peak, 0.20 off.
func tariffTOU20() tariff {
	t := tariffTOU()
	t.currency = "EUR"
	return t
}

func tree(t *testing.T, v any) any {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	var out any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func compare(path string, got, want any, diffs *[]string) {
	switch w := want.(type) {
	case map[string]any:
		g, ok := got.(map[string]any)
		if !ok {
			*diffs = append(*diffs, fmt.Sprintf("%s: got %v, want an object", path, got))
			return
		}
		for k := range w {
			compare(path+"."+k, g[k], w[k], diffs)
		}
		for k := range g {
			if _, ok := w[k]; !ok {
				*diffs = append(*diffs, fmt.Sprintf("%s.%s: not in the Python's", path, k))
			}
		}
	case []any:
		g, ok := got.([]any)
		if !ok || len(g) != len(w) {
			*diffs = append(*diffs, fmt.Sprintf("%s: got %v, want %v", path, got, want))
			return
		}
		for i := range w {
			compare(fmt.Sprintf("%s[%d]", path, i), g[i], w[i], diffs)
		}
	default:
		if fmt.Sprint(got) != fmt.Sprint(want) {
			*diffs = append(*diffs, fmt.Sprintf("%s: got %v, want %v", path, got, want))
		}
	}
}

func TestReportFilesAndNoRows(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"2026-09/2026-09-23.csv", "2026-09/2026-09-24.csv"} {
		raw, err := os.ReadFile(filepath.Join("testdata/pmlog", name))
		if err != nil {
			t.Fatal(err)
		}
		os.MkdirAll(filepath.Join(dir, "2026-09"), 0o755)
		os.WriteFile(filepath.Join(dir, name), raw, 0o644)
	}
	path, err := writeReport(dir, tariffTOU20(), "6-13,15-22", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	page, _ := os.ReadFile(path)
	if !strings.Contains(string(page), `const DATA = {"cfg":{"currency":"EUR"`) || strings.Contains(string(page), "__DATA__") {
		t.Errorf("the data is not in the page: %.300s", page[strings.Index(string(page), "const DATA"):])
	}
	if strings.Contains(string(page), `url("fonts/`) || strings.Count(string(page), "data:font/woff2;base64,") != 3 {
		t.Error("the three fonts are not inside the page")
	}
	for _, old := range []string{"#e8873a", "ShinyWidgets", `class="tile"`} {
		if strings.Contains(string(page), old) {
			t.Errorf("the old look is still in the page: %s", old)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "report_data.json")); err != nil {
		t.Error("report_data.json not written")
	}
	if _, err := writeReport(t.TempDir(), tariffTOU20(), "", time.Now()); err != errNoRows {
		t.Errorf("an empty log: %v", err)
	}
}
