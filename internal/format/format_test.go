package format_test

import (
	"context"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/corygyarmathy/slipbox/internal/fallback"
	"github.com/corygyarmathy/slipbox/internal/format"
	"github.com/corygyarmathy/slipbox/internal/formatcheck"
	"github.com/corygyarmathy/slipbox/internal/run"
	"github.com/corygyarmathy/slipbox/internal/source"
	"github.com/corygyarmathy/slipbox/internal/split"
	"github.com/corygyarmathy/slipbox/internal/striptraces"
	"github.com/corygyarmathy/slipbox/internal/testfixture"
)

// atStripped makes a run folder for src whose step 2.2 is complete, with
// stripped, keyed by `<n> <title>/<note>`, as its stripped notes.
func atStripped(t *testing.T, src *source.Source, stripped map[string]string) *run.Folder {
	t.Helper()
	folder, err := run.New(filepath.Join(t.TempDir(), "run"))
	if err != nil {
		t.Fatal(err)
	}
	if err := folder.SaveSource(src.Name, []byte(src.Frame)); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(folder.Dir, filepath.FromSlash(run.StrippedNotes))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for rel, body := range stripped {
		p := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return folder
}

// stage3Prompt is segment n's user prompt as the run built it after the
// frame, from the stripped notes' file text: the notes block of
// every note in the segment's folder, dir, in sorted order.
func stage3Prompt(stripped map[string]string, dir string, n int) string {
	var names []string
	for rel := range stripped {
		if strings.HasPrefix(rel, dir+"/") {
			names = append(names, rel)
		}
	}
	slices.Sort(names)
	label := "SEGMENT " + strconv.Itoa(n) + " STAGE 2 NOTES"
	p := "=== " + label + " ===\n"
	for _, rel := range names {
		p += "--- NOTE: " + strings.TrimPrefix(rel, dir+"/") + " ---\n" + stripped[rel] + "\n"
	}
	return p + "=== END " + label + " ===\n\n"
}

// TestRun_ReplaysThePragmatismFormat runs step 2.2 on the notes the
// Pragmatism run's step 2.1 gates passed on, then step 3.1 with the run's
// recorded replies. Each call is the request the run made, so each
// segment's folder holds the run's formatted notes byte for byte, its
// manifest is kept, and no segment falls back. Every call is sent
// stage3Prompt's prompt of the stripped notes, not the traced ones, and the
// stage 3 brief and the frame as its system prompt, at the step's tier and
// effort.
func TestRun_ReplaysThePragmatismFormat(t *testing.T) {
	src, recorded := testfixture.PragmatismSource(t), testfixture.PragmatismRun(t)
	folder := testfixture.PragmatismFolder(t, run.SplitGatesDir)
	if err := striptraces.Run(src, folder); err != nil {
		t.Fatal(err)
	}
	stripped := testfixture.Files(t, filepath.Join(folder.Dir, filepath.FromSlash(run.StrippedNotes)))

	var prompts []string
	for n, seg := range src.Segments {
		prompts = append(prompts, stage3Prompt(stripped, split.Folder(seg), n))
	}
	step := filepath.Join(recorded, run.FormatDir)
	p := testfixture.Replay(t, prompts, testfixture.RawReplies(t, step, len(src.Segments)))
	if err := format.Run(context.Background(), p, src, format.DefaultModel, folder); err != nil {
		t.Fatal(err)
	}
	testfixture.SameRequests(t, filepath.Join(folder.Dir, run.FormatDir), step, "")

	calls := p.Calls()
	if len(calls) != len(src.Segments) {
		t.Errorf("%d calls, want one per segment", len(calls))
	}
	frame := testfixture.PragmatismFrame(t)
	wantSystem := testfixture.RepoFile(t, "internal/briefs/stage-3.md") + "\n\n=== FRAME: " + testfixture.Pragmatism + ".md ===\n" + frame + "\n=== END FRAME ===\n"
	for _, c := range calls {
		if c.Model != "haiku" || c.Effort != "medium" || c.System != wantSystem {
			t.Errorf("call for %.40q: model %q at %q, or its system prompt is not stage-3.md and the frame", c.User, c.Model, c.Effort)
		}
		// A stripped note's one footnote is the source footnote, [^1].
		if strings.Contains(c.User, "[^2]") || !strings.Contains(c.User, "[^1]: "+src.CitekeyLink+"\n") {
			t.Errorf("call for %.40q: its notes are not the stripped ones", c.User)
		}
	}

	got := testfixture.Files(t, filepath.Join(folder.Dir, run.FormatDir))
	want := testfixture.Files(t, step)
	for _, dir := range []string{run.Stage3NotesDir, run.ManifestsDir, run.RawDir} {
		g, w := under(got, dir), under(want, dir)
		if !reflect.DeepEqual(slices.Sorted(maps.Keys(g)), slices.Sorted(maps.Keys(w))) {
			t.Errorf("%s/ holds %q, want the run's %q", dir, slices.Sorted(maps.Keys(g)), slices.Sorted(maps.Keys(w)))
			continue
		}
		for name, text := range w {
			if g[name] != text {
				t.Errorf("%s/%s is not the run's", dir, name)
			}
		}
	}
	if n := len(under(got, run.Stage3NotesDir)); n != len(stripped) {
		t.Errorf("%d formatted notes, want one per stripped note (%d)", n, len(stripped))
	}
	if items, err := fallback.Read(folder, run.FormatDir); err != nil || len(items) != 0 {
		t.Errorf("fallbacks %+v (err %v), want none", items, err)
	}
	if n := strings.Count(got[run.UsageFile], `"brief":"stage-3.md"`); n != len(src.Segments) {
		t.Errorf("usage log names stage-3.md %d times, want once per segment:\n%s", n, got[run.UsageFile])
	}
}

// under returns the entries of files under dir/, keyed by the rest of the
// path.
func under(files map[string]string, dir string) map[string]string {
	out := map[string]string{}
	for k, v := range files {
		if rest, ok := strings.CutPrefix(k, dir+"/"); ok {
			out[rest] = v
		}
	}
	return out
}

const tinySource = `---
source: "[[@Tiny - A. Author]]"
---
# 1. Tiny

Alpha is first.

## Two

Gamma is third.
`

// tinyStripped is segment 0's stripped notes; segment 1 has none.
var tinyStripped = map[string]string{
	"0 Tiny/Alpha Is First.md": "# Alpha Is First[^1]\n\nAlpha is first.\n\n[^1]: [[@Tiny - A. Author]]\n",
	"0 Tiny/Tiny.md":           "# Tiny\n\n1. [[Alpha Is First]]\n",
}

// "No change" is a correct result, and a name the reply gives without .md
// gets it. A segment with no folder of
// stripped notes gets no call.
func TestRun_NoChange(t *testing.T) {
	src, err := source.Parse("Tiny", tinySource)
	if err != nil {
		t.Fatal(err)
	}
	folder := atStripped(t, src, tinyStripped)
	reply := "=== FILE: Alpha Is First ===\n" + tinyStripped["0 Tiny/Alpha Is First.md"] +
		"=== FILE: Tiny.md ===\n" + tinyStripped["0 Tiny/Tiny.md"] +
		"=== MANIFEST ===\n{\"changes\": []}"
	p := testfixture.Replay(t, []string{stage3Prompt(tinyStripped, "0 Tiny", 0)}, []string{reply})
	if err := format.Run(context.Background(), p, src, "sonnet", folder); err != nil {
		t.Fatal(err)
	}
	if c := p.Calls(); len(c) != 1 || c[0].Model != "sonnet" {
		t.Fatalf("calls %+v, want one, to the model asked for", c)
	}
	got := testfixture.Files(t, filepath.Join(folder.Dir, run.FormatDir))
	want := []string{"fallbacks.json", "manifests/0.json", "raw/0.txt", "requests/0.sha256", "stage-3/0 Tiny/Alpha Is First.md", "stage-3/0 Tiny/Tiny.md", "usage.jsonl"}
	if keys := slices.Sorted(maps.Keys(got)); !reflect.DeepEqual(keys, want) {
		t.Fatalf("step wrote %q, want %q", keys, want)
	}
	// The reply's manifest, which ends with no newline, is kept as the runner
	// parses its blocks: trimmed, with one trailing newline.
	if m := got["manifests/0.json"]; m != "{\"changes\": []}\n" {
		t.Errorf("manifest %q, want the reply's trimmed, with one trailing newline", m)
	}
	for rel, body := range tinyStripped {
		if got["stage-3/"+rel] != body {
			t.Errorf("%s changed: %q", rel, got["stage-3/"+rel])
		}
	}
}

// A reply that isn't whole notes with plain names, or whose notes aren't
// the segment's, each once, falls back: the step completes with no folder
// or manifest for its segment, and lists the segment and the reply's fault.
func TestRun_ABadReplyFallsBack(t *testing.T) {
	src, err := source.Parse("Tiny", tinySource)
	if err != nil {
		t.Fatal(err)
	}
	const tail = "=== MANIFEST ===\n{}\n"
	const whole = ", but it must give back every note of its segment, as named"
	for name, c := range map[string]struct{ reply, why string }{
		"escape":    {"=== FILE: ../Alpha Is First.md ===\nx\n=== FILE: Tiny.md ===\nhub\n" + tail, `named a note "../Alpha Is First.md", not a plain file name`},
		"delete":    {"=== DELETE: Alpha Is First.md ===\n=== FILE: Tiny.md ===\nhub\n" + tail, `gave "Alpha Is First.md" as a delete or edits, not a whole note`},
		"edit":      {"=== EDIT: Alpha Is First.md ===\nx\n=== FILE: Tiny.md ===\nhub\n" + tail, `gave "Alpha Is First.md" as a delete or edits, not a whole note`},
		"twice":     {"=== FILE: Alpha Is First ===\nx\n=== FILE: Alpha Is First.md ===\ny\n=== FILE: Tiny.md ===\nhub\n" + tail, `wrote "Alpha Is First.md" twice`},
		"missing":   {"=== FILE: Alpha Is First.md ===\nalpha\n" + tail, `left out ["Tiny.md"]` + whole},
		"extra":     {"=== FILE: Alpha Is First.md ===\nalpha\n=== FILE: Tiny.md ===\nhub\n=== FILE: Beta.md ===\nbeta\n" + tail, `wrote ["Beta.md"], not notes of the segment` + whole},
		"renamed":   {"=== FILE: Alpha Comes First.md ===\nalpha\n=== FILE: Tiny.md ===\nhub\n" + tail, `left out ["Alpha Is First.md"] and wrote ["Alpha Comes First.md"], not notes of the segment` + whole},
		"no notes":  {tail, `left out ["Alpha Is First.md" "Tiny.md"]` + whole},
		"no blocks": {"No change.\n", `left out ["Alpha Is First.md" "Tiny.md"]` + whole},
	} {
		t.Run(name, func(t *testing.T) {
			folder := atStripped(t, src, tinyStripped)
			p := testfixture.Replay(t, []string{stage3Prompt(tinyStripped, "0 Tiny", 0)}, []string{c.reply})
			if err := format.Run(context.Background(), p, src, format.DefaultModel, folder); err != nil {
				t.Fatal(err)
			}
			got := testfixture.Files(t, filepath.Join(folder.Dir, run.FormatDir))
			if keys, want := slices.Sorted(maps.Keys(got)), []string{"fallbacks.json", "raw/0.txt", "requests/0.sha256", "usage.jsonl"}; !reflect.DeepEqual(keys, want) {
				t.Errorf("step wrote %q, want %q", keys, want)
			}
			items, err := fallback.Read(folder, run.FormatDir)
			if err != nil {
				t.Fatal(err)
			}
			if want := []fallback.Item{{Segment: 0, Why: "step 3.1's reply " + c.why}}; !reflect.DeepEqual(items, want) {
				t.Errorf("fallbacks %+v, want %+v", items, want)
			}
		})
	}
}

// The step doesn't run until step 2.2 is complete: it makes no call and
// writes no directory.
func TestRun_NotStripped(t *testing.T) {
	src, err := source.Parse("Tiny", tinySource)
	if err != nil {
		t.Fatal(err)
	}
	folder := atStripped(t, src, tinyStripped)
	if err := os.Rename(filepath.Join(folder.Dir, run.StripDir), filepath.Join(folder.Dir, run.StripDir+run.PartialSuffix)); err != nil {
		t.Fatal(err)
	}
	p := testfixture.Replay(t, nil, nil)
	if err := format.Run(context.Background(), p, src, format.DefaultModel, folder); err == nil {
		t.Errorf("no error")
	}
	if c := p.Calls(); len(c) != 0 {
		t.Errorf("%d calls, want none", len(c))
	}
	for _, name := range []string{run.FormatDir, run.FormatDir + run.PartialSuffix} {
		if _, err := os.Stat(filepath.Join(folder.Dir, name)); !os.IsNotExist(err) {
			t.Errorf("%s exists (stat: %v)", name, err)
		}
	}
}

// When every segment's reply falls back, step 3.1 still writes its
// `stage-3/`, empty, so its gate keeps every segment's stripped notes rather
// than failing to read it.
func TestRun_EveryReplyFallsBackThenGate(t *testing.T) {
	src, err := source.Parse("Tiny", tinySource)
	if err != nil {
		t.Fatal(err)
	}
	reply := "=== FILE: Alpha Is First ===\nx\n=== FILE: Alpha Is First.md ===\ny\n=== FILE: Tiny.md ===\nhub\n=== MANIFEST ===\n{}\n"
	folder := atStripped(t, src, tinyStripped)
	p := testfixture.Replay(t, []string{stage3Prompt(tinyStripped, "0 Tiny", 0)}, []string{reply})
	if err := format.Run(context.Background(), p, src, format.DefaultModel, folder); err != nil {
		t.Fatal(err)
	}
	if err := formatcheck.Run(src, folder); err != nil {
		t.Fatalf("formatcheck.Run: %v", err)
	}
	got := testfixture.Files(t, filepath.Join(folder.Dir, filepath.FromSlash(run.FormattedOrFallbackNotes)))
	want := testfixture.Files(t, filepath.Join(folder.Dir, filepath.FromSlash(run.StrippedNotes)))
	if !reflect.DeepEqual(got, want) {
		t.Errorf("notes that go on %q, want the stripped notes %q", got, want)
	}
}
