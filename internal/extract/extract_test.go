package extract_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/corygyarmathy/slipbox/internal/dropped"
	"github.com/corygyarmathy/slipbox/internal/extract"
	"github.com/corygyarmathy/slipbox/internal/note"
	"github.com/corygyarmathy/slipbox/internal/provider"
	"github.com/corygyarmathy/slipbox/internal/run"
	"github.com/corygyarmathy/slipbox/internal/source"
	"github.com/corygyarmathy/slipbox/internal/testfixture"
)

// TestPrompt pins each segment's user prompt on the invented chapter: its
// number, its title, which drops the heading's number and straightens its
// apostrophe, its heading as written, the clause segment 0 gets for the
// text before the first heading, and the source file name for traces.
func TestPrompt(t *testing.T) {
	src := testfixture.LampSource(t)
	for _, seg := range src.Segments {
		testfixture.Golden(t, "prompt/"+strconv.Itoa(seg.Number)+".txt", extract.Prompt(src, seg))
	}
}

// replyCase is a segment's reply and what Run should write from it.
type replyCase struct {
	name    string
	segment int
	reply   string
	want    segmentWant
}

// segmentWant is what Run writes for one segment: its notes by file name,
// its report, and what it dropped.
type segmentWant struct {
	notes   map[string]string
	report  string
	dropped []dropped.Item
}

// lampTrace is a footnote definition line: label's trace of quote into the
// invented chapter's section.
func lampTrace(label int, section, quote string) string {
	return fmt.Sprintf("[^%d]: %s\n", label, testfixture.LampTrace(section, quote))
}

// renameLine is the line Run adds to a segment's report for a note it
// renamed from from to to.
func renameLine(from, to string) string {
	return "\ns2n renamed `" + from + "` to `" + to + "`, and set its H1 to `# " + strings.TrimSuffix(to, ".md") +
		"`: a segment's notes are named with its number.\n"
}

// badName is why Run drops a file named name, which isn't a note's: the
// note package's reason.
func badName(t *testing.T, name string) string {
	t.Helper()
	_, err := note.FileName(name)
	if err == nil {
		t.Fatalf("%q is a note's name", name)
	}
	return err.Error()
}

// replyCases are one quote-once reply per segment of the invented chapter,
// each covering shapes of reply the step handles. Run makes one call per
// segment, so a case that covers several deviations gives them in one
// reply, and its name says which.
func replyCases(t *testing.T) []replyCase {
	return []replyCase{
		// A well-formed reply: notes named with the segment's number, their
		// statements written quote-once, which s2n traces into the
		// segment's own section, joining text and a `{}` cut included. An
		// aside named with its number keeps the H1 it was given, which
		// isn't its name, and gets no rename line. The report is kept as
		// given.
		{"well-formed, quote-once", 0, "=== FILE: 0 Keepers of the Lamp.md ===\n# 0 Keepers of the Lamp\n\n" +
			"A lighthouse is a promise made to ships{, and} it burns whether or not a ship is near.\n" +
			"The keeper kept that promise{} for the harbour.\n" +
			"=== FILE: 0 Keepers of the Lamp - Aside - Promises.md ===\n# Promises\n\n" +
			"The keeper kept that promise for the harbour.\n" +
			"=== REPORT ===\nNo repairs.\n",
			segmentWant{
				notes: map[string]string{
					"0 Keepers of the Lamp.md": "# 0 Keepers of the Lamp\n\n" +
						"A lighthouse is a promise made to ships,[^1] and it burns whether or not a ship is near.[^2]\n" +
						"The keeper kept that promise[^3] for the harbour.[^4]\n\n" +
						lampTrace(1, "2. Keepers of the Lamp", "A lighthouse is a promise made to ships") +
						lampTrace(2, "2. Keepers of the Lamp", "It burns whether or not a ship is near.") +
						lampTrace(3, "2. Keepers of the Lamp", "The keeper kept that promise") +
						lampTrace(4, "2. Keepers of the Lamp", "for the harbour."),
					"0 Keepers of the Lamp - Aside - Promises.md": "# Promises\n\n" +
						"The keeper kept that promise for the harbour.[^1]\n\n" +
						lampTrace(1, "2. Keepers of the Lamp", "The keeper kept that promise for the harbour."),
				},
				report: "No repairs.\n",
			}},
		// A main note named with a chapter heading's numbering, `4. `, gets
		// the segment's number in its place, and an aside named with no
		// number and no `.md` gets it in front: each with its H1 set to the
		// new name, and a line for each in the report. The aside is traced
		// by the generator, with no braces, and is written as given. The
		// main note traces into a heading with a curly apostrophe its title
		// straightens.
		{"renamed main note and aside, a note the generator traced", 1, "=== FILE: 4. The Keeper's Watch.md ===\n# 4. The Keeper's Watch\n\n" +
			"The watch began at dusk.\n" +
			"The keeper{} trimmed the wick every four hours.\n" +
			"=== FILE: The Keeper's Watch - Aside - The log ===\n# The Keeper's Watch - Aside - The log\n\n" +
			"The log was read by nobody until a ship was lost.[^1]\n\n" +
			lampTrace(1, "The Keeper’s Watch", "The log was read by nobody until a ship was lost.") +
			"=== REPORT ===\nNo repairs.\n",
			segmentWant{
				notes: map[string]string{
					"1 The Keeper's Watch.md": "# 1 The Keeper's Watch\n\n" +
						"The watch began at dusk.[^1]\n" +
						"The keeper[^2] trimmed the wick every four hours.[^3]\n\n" +
						lampTrace(1, "The Keeper’s Watch", "The watch began at dusk.") +
						lampTrace(2, "The Keeper’s Watch", "The keeper") +
						lampTrace(3, "The Keeper’s Watch", "trimmed the wick every four hours"),
					"1 The Keeper's Watch - Aside - The log.md": "# 1 The Keeper's Watch - Aside - The log\n\n" +
						"The log was read by nobody until a ship was lost.[^1]\n\n" +
						lampTrace(1, "The Keeper’s Watch", "The log was read by nobody until a ship was lost."),
				},
				report: "No repairs.\n" +
					renameLine("4. The Keeper's Watch.md", "1 The Keeper's Watch.md") +
					renameLine("The Keeper's Watch - Aside - The log.md", "1 The Keeper's Watch - Aside - The log.md"),
			}},
		// Every file Run can't write is dropped and listed, and the step
		// goes on to the next: a name written twice, the second time
		// without `.md`; a rename to a name the reply already wrote; names
		// outside the notes directory; and a DELETE, which only a repair
		// call may make. The first `2 The Lens.md` is the one written.
		{"name written twice, rename to a doubled name, bad names, delete", 2, "=== FILE: 2 The Lens.md ===\n# 2 The Lens\n\n" +
			"The lens bends light into a beam.\n" +
			"=== FILE: 2 The Lens ===\n# 2 The Lens\n\nA beam carries farther than a glow.\n" +
			"=== FILE: 4 The Lens.md ===\n# 4 The Lens\n\nA beam carries farther than a glow.\n" +
			"=== FILE: ../escape.md ===\nx\n" +
			"=== FILE: a/b.md ===\nx\n" +
			"=== FILE: .. ===\nx\n" +
			"=== DELETE: 2 The Lens - Aside - Prisms.md ===\n" +
			"=== FILE: Glass.md ===\n# Glass\n\nA beam carries farther than a glow.\n" +
			"=== REPORT ===\nDropped the Fresnel lens, as detail.\n",
			segmentWant{
				notes: map[string]string{
					"2 The Lens.md": "# 2 The Lens\n\n" +
						"The lens bends light into a beam.[^1]\n\n" +
						lampTrace(1, "The Lens", "The lens bends light into a beam."),
					"2 Glass.md": "# 2 Glass\n\n" +
						"A beam carries farther than a glow.[^1]\n\n" +
						lampTrace(1, "The Lens", "A beam carries farther than a glow."),
				},
				report: "Dropped the Fresnel lens, as detail.\n" + renameLine("Glass.md", "2 Glass.md"),
				dropped: []dropped.Item{
					{Segment: 2, Note: "2 The Lens.md", What: "the note", Why: "the reply writes it twice"},
					{Segment: 2, Note: "2 The Lens.md", What: "the note", Why: `renamed from "4 The Lens.md", it is a name the reply already wrote`},
					{Segment: 2, Note: "../escape.md", What: "the note", Why: badName(t, "../escape.md")},
					{Segment: 2, Note: "a/b.md", What: "the note", Why: badName(t, "a/b.md")},
					{Segment: 2, Note: "..", What: "the note", Why: badName(t, "..")},
					{Segment: 2, Note: "2 The Lens - Aside - Prisms.md", What: "the note", Why: "the reply deletes it, which only a repair call may do"},
				},
			}},
		// A note named with no number and with no H1 gets the number, and
		// an H1. A statement sets the author's name, from the `source:`
		// line, into a quote, as the generator writes `I, <the author>,`:
		// the name is traced to the source, and the quote either side is
		// one. `Glass.md`, which segment 2 wrote too, is kept under this
		// segment's number: each segment's names are its own.
		{"no number and no H1, the author's name in a quote, a name another segment wrote", 3, "=== FILE: Storms.md ===\n" +
			"In a storm the light matters most.\n" +
			"I{, }Rhea Vale{, }have stood in that lamp room in a gale, and the glass shook.\n" +
			"=== FILE: Glass.md ===\n# Glass\n\nAnd the glass shook.\n" +
			"=== REPORT ===\nr\n",
			segmentWant{
				notes: map[string]string{
					"3 Storms.md": "# 3 Storms\n" +
						"In a storm the light matters most.[^1]\n" +
						"I, Rhea Vale,[^2] have stood in that lamp room in a gale, and the glass shook.[^3]\n\n" +
						lampTrace(1, "Storms", "In a storm the light matters most.") +
						"[^2]: [[" + testfixture.Lamp + "]] \"Rhea Vale\"\n" +
						lampTrace(3, "Storms", "I have stood in that lamp room in a gale, and the glass shook."),
					"3 Glass.md": "# 3 Glass\n\n" +
						"And the glass shook.[^1]\n\n" +
						lampTrace(1, "Storms", "and the glass shook."),
				},
				report: "r\n" + renameLine("Storms.md", "3 Storms.md") + renameLine("Glass.md", "3 Glass.md"),
			}},
	}
}

// TestRun runs step 1.1 quote-once, the default, on the invented chapter,
// serving each segment its reply case, and checks the requests it made and
// the notes, reports, raw replies and drops it wrote.
func TestRun(t *testing.T) {
	src := testfixture.LampSource(t)
	cases := replyCases(t)
	var serve []testfixture.Case
	for _, c := range cases {
		serve = append(serve, testfixture.Case{Contains: fmt.Sprintf("Your segment: number %d, ", c.segment), Reply: c.reply})
	}
	p := testfixture.Serve(t, serve...)
	folder, err := run.New(filepath.Join(t.TempDir(), "run"))
	if err != nil {
		t.Fatal(err)
	}
	if err := extract.Run(context.Background(), p, src, extract.DefaultModel, extract.DefaultEffort, true, folder); err != nil {
		t.Fatal(err)
	}

	files := testfixture.Files(t, filepath.Join(folder.Dir, run.ExtractDir))
	drops := droppedItems(t, folder)
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			n := strconv.Itoa(c.segment)
			if got := files[run.RawDir+"/"+n+".txt"]; got != c.reply {
				t.Errorf("segment %d: raw reply is not the reply:\n got %q\nwant %q", c.segment, got, c.reply)
			}
			if got := files[run.ReportsDir+"/"+n+".md"]; got != c.want.report {
				t.Errorf("segment %d: report:\n got %q\nwant %q", c.segment, got, c.want.report)
			}
			got := map[string]string{}
			for name, text := range under(files, run.NotesDir) {
				if base := strings.TrimPrefix(name, run.NotesDir+"/"); strings.HasPrefix(base, n+" ") {
					got[base] = text
				}
			}
			if !reflect.DeepEqual(got, c.want.notes) {
				t.Errorf("segment %d: notes:\n got %q\nwant %q", c.segment, got, c.want.notes)
			}
			var gotDrops []dropped.Item
			for _, d := range drops {
				if d.Segment == c.segment {
					gotDrops = append(gotDrops, d)
				}
			}
			if !reflect.DeepEqual(gotDrops, c.want.dropped) {
				t.Errorf("segment %d: dropped:\n got %+v\nwant %+v", c.segment, gotDrops, c.want.dropped)
			}
		})
	}
	var want int
	for _, c := range cases {
		want += len(c.want.notes)
	}
	if notes := under(files, run.NotesDir); len(notes) != want {
		t.Errorf("%d notes written, want the cases' %d", len(notes), want)
	}

	brief := testfixture.RepoFile(t, "internal/briefs/stage-1-quote-once.md")
	checkCalls(t, p.Calls(), src, brief)
	if n := strings.Count(files[run.UsageFile], `"brief":"stage-1-quote-once.md"`); n != len(src.Segments) {
		t.Errorf("%d usage records name the quote-once brief, want one per segment (%d)", n, len(src.Segments))
	}
}

// TestRun_WithoutQuoteOnce runs step 1.1 as trialled before quote-once: the
// generator is briefed to write the traces, and its notes are written as
// given, braces too, as s2n traces nothing.
func TestRun_WithoutQuoteOnce(t *testing.T) {
	src := testfixture.LampSource(t)
	traced := "# 0 Keepers of the Lamp\n\nA lighthouse is a promise made to ships.[^1]\n\n" +
		lampTrace(1, "2. Keepers of the Lamp", "A lighthouse is a promise made to ships.")
	braced := "# 1 The Keeper's Watch\n\nThe keeper{} trimmed the wick every four hours.\n"
	p := testfixture.Serve(t,
		testfixture.Case{Contains: "Your segment: number 0, ", Reply: "=== FILE: 0 Keepers of the Lamp.md ===\n" + traced + "=== REPORT ===\nr\n"},
		testfixture.Case{Contains: "Your segment: number 1, ", Reply: "=== FILE: 1 The Keeper's Watch.md ===\n" + braced + "=== REPORT ===\nr\n"},
		testfixture.Case{Contains: "Your segment: number 2, ", Reply: "=== REPORT ===\nr\n"},
		testfixture.Case{Contains: "Your segment: number 3, ", Reply: "=== REPORT ===\nr\n"},
	)
	folder, err := run.New(filepath.Join(t.TempDir(), "run"))
	if err != nil {
		t.Fatal(err)
	}
	if err := extract.Run(context.Background(), p, src, extract.DefaultModel, extract.DefaultEffort, false, folder); err != nil {
		t.Fatal(err)
	}
	files := testfixture.Files(t, filepath.Join(folder.Dir, run.ExtractDir))
	want := map[string]string{
		run.NotesDir + "/0 Keepers of the Lamp.md": traced,
		run.NotesDir + "/1 The Keeper's Watch.md":  braced,
	}
	if got := under(files, run.NotesDir); !reflect.DeepEqual(got, want) {
		t.Errorf("notes:\n got %q\nwant %q", got, want)
	}

	brief := testfixture.RepoFile(t, "internal/briefs/stage-1.md")
	checkCalls(t, p.Calls(), src, brief)
	if n := strings.Count(files[run.UsageFile], `"brief":"stage-1.md"`); n != len(src.Segments) {
		t.Errorf("%d usage records name stage-1.md, want one per segment (%d)", n, len(src.Segments))
	}
}

// checkCalls checks the requests of a step 1.1 run on the invented chapter:
// one call per segment, the first segment 0's, each with brief and the
// frame as its system prompt, at opus and medium, and with its segment's
// prompt.
func checkCalls(t *testing.T, calls []provider.Request, src *source.Source, brief string) {
	t.Helper()
	wantSystem := brief + "\n\n=== FRAME: " + testfixture.Lamp + ".md ===\n" + testfixture.LampChapter + "\n=== END FRAME ===\n"
	if len(calls) != len(src.Segments) {
		t.Fatalf("%d calls, want one per segment (%d)", len(calls), len(src.Segments))
	}
	var users []string
	for _, c := range calls {
		if c.System != wantSystem {
			t.Errorf("call for %q: system prompt is not the brief and the frame", c.User)
		}
		if c.Model != "opus" || c.Effort != "medium" {
			t.Errorf("call for %q: model %q at effort %q, want opus at medium", c.User, c.Model, c.Effort)
		}
		users = append(users, c.User)
	}
	for _, seg := range src.Segments {
		if want := extract.Prompt(src, seg); !slices.Contains(users, want) {
			t.Errorf("%s: no call has its prompt %q", seg, want)
		}
	}
	if want := extract.Prompt(src, src.Segments[0]); calls[0].User != want {
		t.Errorf("first call was not segment 0's: %q", calls[0].User)
	}
}

// failSegment serves an empty reply to every prompt except one, which fails.
type failSegment struct{ prompt string }

func (f failSegment) Call(ctx context.Context, req provider.Request) (provider.Reply, error) {
	if req.User == f.prompt {
		return provider.Reply{}, errors.New("interrupted")
	}
	return provider.Reply{Text: "=== REPORT ===\nok\n"}, ctx.Err()
}

func TestRun_InterruptedStepLeavesNoCompleteDirectory(t *testing.T) {
	src := testfixture.LampSource(t) // 4 segments
	tests := map[string]provider.Provider{
		"segment 0 fails": failSegment{extract.Prompt(src, src.Segments[0])},
		"segment 3 fails": failSegment{extract.Prompt(src, src.Segments[3])},
	}
	for name, p := range tests {
		t.Run(name, func(t *testing.T) {
			folder, err := run.New(filepath.Join(t.TempDir(), "run"))
			if err != nil {
				t.Fatal(err)
			}
			if err := extract.Run(context.Background(), p, src, extract.DefaultModel, extract.DefaultEffort, false, folder); err == nil {
				t.Fatal("Run succeeded, want the failure")
			}
			if _, err := os.Stat(filepath.Join(folder.Dir, run.ExtractDir)); !errors.Is(err, os.ErrNotExist) {
				t.Errorf("complete step directory exists after a failure (stat err %v)", err)
			}
		})
	}
	t.Run("cancelled", func(t *testing.T) {
		folder, err := run.New(filepath.Join(t.TempDir(), "run"))
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if err := extract.Run(ctx, provider.NewReplay(nil), src, extract.DefaultModel, extract.DefaultEffort, false, folder); err == nil {
			t.Fatal("Run succeeded, want the cancellation")
		}
		if _, err := os.Stat(filepath.Join(folder.Dir, run.ExtractDir)); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("complete step directory exists after cancellation (stat err %v)", err)
		}
	})
}

// droppedItems reads what step 1.1 dropped in folder.
func droppedItems(t *testing.T, folder *run.Folder) []dropped.Item {
	t.Helper()
	items, err := dropped.Read(folder, run.ExtractDir)
	if err != nil {
		t.Fatal(err)
	}
	if !folder.Complete(run.ExtractDir) {
		t.Fatal("step 1.1 is not complete")
	}
	return items
}

// under gives the files of files under dir.
func under(files map[string]string, dir string) map[string]string {
	out := map[string]string{}
	for name, text := range files {
		if strings.HasPrefix(name, dir+"/") {
			out[name] = text
		}
	}
	return out
}

func TestSegmentName(t *testing.T) {
	tests := []struct {
		n                 int
		title, name, want string
	}{
		{0, "Tiny", "0 Tiny.md", "0 Tiny.md"},
		{0, "Tiny", "4 Tiny.md", "0 Tiny.md"},
		{0, "Tiny", "4. Tiny.md", "0 Tiny.md"},
		{0, "Tiny", "Tiny.md", "0 Tiny.md"},
		{0, "Tiny", "0. Tiny.md", "0 Tiny.md"},
		{12, "Tiny", "1 Tiny.md", "12 Tiny.md"},
		{1, "Tiny", "12 Tiny.md", "1 Tiny.md"},
		{0, "Tiny", "4.5 Tiny.md", "0 4.5 Tiny.md"},
		{0, "Tiny", "4Tiny.md", "0 4Tiny.md"},
		{3, "2 Kinds of Good", "2 Kinds of Good.md", "3 2 Kinds of Good.md"},
		{3, "2 Kinds of Good", "2 Kinds of Good - Aside - X.md", "3 2 Kinds of Good - Aside - X.md"},
		{3, "2 Kinds of Good", "4 2 Kinds of Good.md", "3 2 Kinds of Good.md"},
	}
	for _, tt := range tests {
		seg := source.Segment{Number: tt.n, Title: tt.title}
		if got := extract.SegmentName(seg, tt.name); got != tt.want {
			t.Errorf("SegmentName(%d %q, %q) = %q, want %q", tt.n, tt.title, tt.name, got, tt.want)
		}
	}
}
