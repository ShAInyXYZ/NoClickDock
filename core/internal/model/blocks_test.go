package model

import (
	"encoding/json"
	"testing"
)

func TestBlocksCarryTheirKind(t *testing.T) {
	got, err := json.Marshal(Blocks{
		Section{Title: "Filesystems"},
		Meter{Label: "/", Frac: 0.5, Tone: ToneOK},
		Split{Columns: []Blocks{{Note{Text: "left"}}, {}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := `[{"kind":"section","title":"Filesystems"},` +
		`{"kind":"meter","label":"/","frac":0.5,"tone":"ok"},` +
		`{"kind":"split","columns":[[{"kind":"note","text":"left"}],[]]}]`
	if string(got) != want {
		t.Fatalf("\n got %s\nwant %s", got, want)
	}
}

func TestTableCellsAreTextUnlessToned(t *testing.T) {
	got, _ := json.Marshal(TableRow{Cells: []Cell{{Text: "GPU0"}, {Text: "92°C", Tone: ToneWarn}}})
	if want := `{"cells":["GPU0",{"text":"92°C","tone":"warn"}]}`; string(got) != want {
		t.Fatalf("got %s, want %s", got, want)
	}
}

func TestEmptyBlocksAreAnArray(t *testing.T) {
	got, _ := json.Marshal(Page{ID: "main"})
	if want := `{"id":"main","label":"","blocks":[]}`; string(got) != want {
		t.Fatalf("got %s, want %s", got, want)
	}
}

func TestStateTone(t *testing.T) {
	for state, tone := range map[State]Tone{
		StateOK: ToneOK, StateWarn: ToneWarn, StateCrit: ToneCrit,
		StateBusy: ToneInfo, StateIdle: ToneIdle, "": ToneIdle,
	} {
		if got := state.Tone(); got != tone {
			t.Errorf("%q.Tone() = %q, want %q", state, got, tone)
		}
	}
}
