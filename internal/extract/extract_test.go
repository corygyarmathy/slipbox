package extract_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/corygyarmathy/slipbox/internal/briefs"
	"github.com/corygyarmathy/slipbox/internal/dropped"
	"github.com/corygyarmathy/slipbox/internal/extract"
	"github.com/corygyarmathy/slipbox/internal/provider"
	"github.com/corygyarmathy/slipbox/internal/run"
	"github.com/corygyarmathy/slipbox/internal/source"
	"github.com/corygyarmathy/slipbox/internal/testfixture"
)

// genPrompt is the trialled stage 1 user prompt, transcribed.
func genPrompt(n int, title, sec, srcname string) string {
	clause := "."
	if n == 0 {
		clause = " (the text before the first `##` heading)."
	}
	return "Your segment: number " + strconv.Itoa(n) + ", title `" + title + "`, section heading `" + sec + "`" +
		clause + " Source file name for traces: `" + srcname + "`."
}

func TestPrompt(t *testing.T) {
	// (title, heading) per segment, the titles the source model must give.
	// The invented chapter's headings have a number and a curly apostrophe,
	// which the titles drop and straighten.
	chapters := map[string]struct {
		src  *source.Source
		segs [][2]string
	}{
		testfixture.Pragmatism: {testfixture.PragmatismSource(t), [][2]string{
			{"What Pragmatism Means", "What Pragmatism Means"},
			{"History of the Method", "History of the Method"},
			{"The Method's Character and Affinities", "The Method's Character and Affinities"},
			{"Pragmatism as a Theory of Truth", "Pragmatism as a Theory of Truth"},
			{"The Formation of New Beliefs", "The Formation of New Beliefs"},
			{"Rationalistic Criticisms", "Rationalistic Criticisms"},
			{"Pragmatism as Mediator Between Empiricism and Religion", "Pragmatism as Mediator Between Empiricism and Religion"},
			{"The True Is the Good in the Way of Belief", "The True Is the Good in the Way of Belief"},
		}},
		testfixture.Lamp: {testfixture.LampSource(t), [][2]string{
			{"Keepers of the Lamp", "2. Keepers of the Lamp"},
			{"The Keeper's Watch", "The Keeper’s Watch"},
			{"The Lens", "The Lens"},
			{"Storms", "Storms"},
		}},
	}
	for name, c := range chapters {
		t.Run(name, func(t *testing.T) {
			var got, want []string
			for _, seg := range c.src.Segments {
				got = append(got, extract.Prompt(c.src, seg))
			}
			for n, s := range c.segs {
				want = append(want, genPrompt(n, s[0], s[1], name))
			}
			if !reflect.DeepEqual(got, want) {
				t.Errorf("prompts:\n got %q\nwant %q", got, want)
			}
		})
	}
	if got, want := genPrompt(0, "What Pragmatism Means", "What Pragmatism Means", testfixture.Pragmatism),
		"Your segment: number 0, title `What Pragmatism Means`, section heading `What Pragmatism Means` (the text before the first `##` heading). Source file name for traces: `Pragmatism - Lecture II - What Pragmatism Means`."; got != want {
		t.Errorf("genPrompt transcription drifted:\n got %q\nwant %q", got, want)
	}
}

// TestRun_ReplaysThePragmatismRun runs step 1.1 quote-once, as the recorded
// run did, with its recorded replies: each call is the request the run made,
// and the step writes one note per segment, traced by s2n.
func TestRun_ReplaysThePragmatismRun(t *testing.T) {
	src := testfixture.PragmatismSource(t)
	recorded := filepath.Join(testfixture.PragmatismRun(t), run.ExtractDir)
	var prompts []string
	for _, seg := range src.Segments {
		prompts = append(prompts, extract.Prompt(src, seg))
	}
	replies := testfixture.RawReplies(t, recorded, len(src.Segments))
	p := testfixture.Replay(t, prompts, replies)
	folder, err := run.New(filepath.Join(t.TempDir(), "run"))
	if err != nil {
		t.Fatal(err)
	}
	if err := extract.Run(context.Background(), p, src, extract.DefaultModel, extract.DefaultEffort, true, folder); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(folder.Dir, run.ExtractDir)
	testfixture.SameRequests(t, dir, recorded, "")

	calls := p.Calls()
	if len(calls) != len(src.Segments) || calls[0].User != prompts[0] {
		t.Fatalf("%d calls, want %d, segment 0's first", len(calls), len(src.Segments))
	}
	wantSystem := briefs.Stage1QuoteOnce + "\n\n=== FRAME: " + testfixture.Pragmatism + ".md ===\n" +
		testfixture.PragmatismFrame(t) + "\n=== END FRAME ===\n"
	for _, c := range calls {
		if c.System != wantSystem || c.Model != "opus" || c.Effort != "medium" {
			t.Errorf("call for %.60q: not the quote-once brief and the frame, at opus and medium", c.User)
		}
	}

	files := testfixture.Files(t, dir)
	notes := under(files, run.NotesDir)
	if len(notes) != len(src.Segments) {
		t.Errorf("%d notes, want one per segment: %d", len(notes), len(src.Segments))
	}
	for _, seg := range src.Segments {
		n := strconv.Itoa(seg.Number)
		if files[run.RawDir+"/"+n+".txt"] != replies[seg.Number] {
			t.Errorf("%s: the raw reply is not kept as received", seg)
		}
		text, ok := notes[run.NotesDir+"/"+n+" "+seg.Title+".md"]
		if !ok {
			t.Errorf("%s: no note named for the segment", seg)
			continue
		}
		// The replies are quote-once, with no footnotes; the notes s2n wrote
		// trace every statement into the segment's own section.
		if strings.Contains(replies[seg.Number], "[^1]:") {
			t.Errorf("%s: the reply has footnotes, so s2n traced nothing", seg)
		}
		if !strings.Contains(text, "[^1]: [["+testfixture.Pragmatism+"#"+seg.Heading+"]] \"") {
			t.Errorf("%s: the note's first trace is not into its section", seg)
		}
	}
	if got := droppedItems(t, folder); len(got) != 0 {
		t.Errorf("dropped %+v, want nothing", got)
	}
	if n := strings.Count(files[run.UsageFile], `"brief":"stage-1-quote-once.md"`); n != len(src.Segments) {
		t.Errorf("%d usage records name the quote-once brief, want %d", n, len(src.Segments))
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

// A file whose name isn't a note's is dropped and listed, and the step goes
// on.
func TestRun_DropsNamesOutsideTheNotesDirectory(t *testing.T) {
	src, err := source.Parse("T", "---\nsource: \"[[@T]]\"\n---\n\n## A\n\nText.\n")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"../escape.md", "a/b.md", ".."} {
		t.Run(name, func(t *testing.T) {
			folder, err := run.New(filepath.Join(t.TempDir(), "run"))
			if err != nil {
				t.Fatal(err)
			}
			p := provider.NewReplay(map[string][]provider.Reply{
				extract.Prompt(src, src.Segments[0]): {{Text: "=== FILE: 0 A.md ===\nx\n=== FILE: " + name + " ===\nx\n=== REPORT ===\nr\n"}},
			})
			if err := extract.Run(context.Background(), p, src, extract.DefaultModel, extract.DefaultEffort, false, folder); err != nil {
				t.Fatal(err)
			}
			got := droppedItems(t, folder)
			if len(got) != 1 || got[0].Note != name || got[0].Segment != 0 {
				t.Errorf("dropped %+v, want %q alone", got, name)
			}
			if notes := under(testfixture.Files(t, filepath.Join(folder.Dir, run.ExtractDir)), run.NotesDir); len(notes) != 1 {
				t.Errorf("notes written: %q, want 0 A.md alone", notes)
			}
		})
	}
}

// A note name written twice in one reply is dropped the second time and
// listed. Two segments can't write the same name: each segment's notes are
// named with its number.
func TestRun_DropsANoteNameWrittenTwice(t *testing.T) {
	src, err := source.Parse("T", "---\nsource: \"[[@T]]\"\n---\n\n## A\n\nText.\n\n## B\n\nText.\n")
	if err != nil {
		t.Fatal(err)
	}
	t.Run("in one reply", func(t *testing.T) {
		folder, err := run.New(filepath.Join(t.TempDir(), "run"))
		if err != nil {
			t.Fatal(err)
		}
		p := provider.NewReplay(map[string][]provider.Reply{
			extract.Prompt(src, src.Segments[0]): {{Text: "=== FILE: 0 A ===\nx\n=== FILE: 0 A.md ===\ny\n=== REPORT ===\nr\n"}},
			extract.Prompt(src, src.Segments[1]): {{Text: "=== REPORT ===\nr\n"}},
		})
		if err := extract.Run(context.Background(), p, src, extract.DefaultModel, extract.DefaultEffort, false, folder); err != nil {
			t.Fatal(err)
		}
		want := []dropped.Item{{Segment: 0, Note: "0 A.md", What: "the note", Why: "the reply writes it twice"}}
		if got := droppedItems(t, folder); !reflect.DeepEqual(got, want) {
			t.Errorf("dropped %+v, want %+v", got, want)
		}
		if got := testfixture.Files(t, filepath.Join(folder.Dir, run.ExtractDir))[run.NotesDir+"/0 A.md"]; got != "x\n" {
			t.Errorf("0 A.md = %q, want the first one written", got)
		}
	})
	t.Run("by two segments", func(t *testing.T) {
		folder, err := run.New(filepath.Join(t.TempDir(), "run"))
		if err != nil {
			t.Fatal(err)
		}
		p := provider.NewReplay(map[string][]provider.Reply{
			extract.Prompt(src, src.Segments[0]): {{Text: "=== FILE: N.md ===\nx\n=== REPORT ===\nr\n"}},
			extract.Prompt(src, src.Segments[1]): {{Text: "=== FILE: N.md ===\ny\n=== REPORT ===\nr\n"}},
		})
		if err := extract.Run(context.Background(), p, src, extract.DefaultModel, extract.DefaultEffort, false, folder); err != nil {
			t.Fatal(err)
		}
		files := testfixture.Files(t, filepath.Join(folder.Dir, run.ExtractDir))
		if files[run.NotesDir+"/0 N.md"] != "# 0 N\nx\n" || files[run.NotesDir+"/1 N.md"] != "# 1 N\ny\n" {
			t.Errorf("the two notes are not kept under their segments' numbers: %q, %q",
				files[run.NotesDir+"/0 N.md"], files[run.NotesDir+"/1 N.md"])
		}
		if got := droppedItems(t, folder); len(got) != 0 {
			t.Errorf("dropped %+v, want nothing", got)
		}
	})
}

// Only a repair call may delete a note: a DELETE line in a generator's reply
// is dropped and listed, and the reply's notes are written.
func TestRun_DropsADelete(t *testing.T) {
	src, err := source.Parse("T", "---\nsource: \"[[@T]]\"\n---\n\n## A\n\nText.\n")
	if err != nil {
		t.Fatal(err)
	}
	folder, err := run.New(filepath.Join(t.TempDir(), "run"))
	if err != nil {
		t.Fatal(err)
	}
	p := provider.NewReplay(map[string][]provider.Reply{
		extract.Prompt(src, src.Segments[0]): {{Text: "=== FILE: 0 A.md ===\nx\n=== DELETE: 0 A - Aside - B.md ===\n=== REPORT ===\nr\n"}},
	})
	if err := extract.Run(context.Background(), p, src, extract.DefaultModel, extract.DefaultEffort, false, folder); err != nil {
		t.Fatal(err)
	}
	got := droppedItems(t, folder)
	if len(got) != 1 || got[0].Note != "0 A - Aside - B.md" || !strings.Contains(got[0].Why, "deletes") {
		t.Errorf("dropped %+v, want the DELETE", got)
	}
	if files := testfixture.Files(t, filepath.Join(folder.Dir, run.ExtractDir)); files[run.NotesDir+"/0 A.md"] != "x\n" {
		t.Errorf("0 A.md not written")
	}
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

// With quote-once, the generator is briefed by the quote-once rules, and the
// step writes the traces of the notes it writes quote-once. The raw reply is
// kept as written.
func TestRun_QuoteOnce(t *testing.T) {
	src, err := source.Parse("T", "---\nsource: \"[[@T - An Author]]\"\n---\n\n## A\n\nThe text is here. More text follows.\n")
	if err != nil {
		t.Fatal(err)
	}
	folder, err := run.New(filepath.Join(t.TempDir(), "run"))
	if err != nil {
		t.Fatal(err)
	}
	reply := "=== FILE: 0 A.md ===\n# 0 A\n\nThe text is here{, and} more text follows.\n=== REPORT ===\nr\n"
	p := provider.NewReplay(map[string][]provider.Reply{extract.Prompt(src, src.Segments[0]): {{Text: reply}}})
	if err := extract.Run(context.Background(), p, src, extract.DefaultModel, extract.DefaultEffort, true, folder); err != nil {
		t.Fatal(err)
	}
	if calls := p.Calls(); len(calls) != 1 || !strings.HasPrefix(calls[0].System, briefs.Stage1QuoteOnce) {
		t.Errorf("the call is not briefed by the quote-once rules")
	}
	files := testfixture.Files(t, filepath.Join(folder.Dir, run.ExtractDir))
	want := "# 0 A\n\nThe text is here,[^1] and more text follows.[^2]\n\n" +
		"[^1]: [[T#A]] \"The text is here\"\n[^2]: [[T#A]] \"More text follows.\"\n"
	if got := files[run.NotesDir+"/0 A.md"]; got != want {
		t.Errorf("note:\n got %q\nwant %q", got, want)
	}
	if files[run.RawDir+"/0.txt"] != reply {
		t.Errorf("the raw reply is not kept as written")
	}
	if !strings.Contains(files[run.UsageFile], `"brief":"stage-1-quote-once.md"`) {
		t.Errorf("usage log does not name the quote-once brief:\n%s", files[run.UsageFile])
	}
}

// A note named without its segment's number gets it, in place of the number
// it has or in front of the name, with its H1 set to the new name, and the
// segment's report says so. A note named with its number is written as given.
func TestRun_SetsTheSegmentNumberOnANoteName(t *testing.T) {
	src, err := source.Parse("T", "---\nsource: \"[[@T]]\"\n---\n\n## Tiny\n\nText.\n\n## B\n\nText.\n")
	if err != nil {
		t.Fatal(err)
	}
	folder, err := run.New(filepath.Join(t.TempDir(), "run"))
	if err != nil {
		t.Fatal(err)
	}
	p := provider.NewReplay(map[string][]provider.Reply{
		extract.Prompt(src, src.Segments[0]): {{Text: "=== FILE: 4 Tiny.md ===\n# 4 Tiny\n\nText.\n" +
			"=== FILE: 4. Tiny - Aside - X ===\n# 4. Tiny - Aside - X\n\nText.\n=== REPORT ===\nNo repairs."}},
		extract.Prompt(src, src.Segments[1]): {{Text: "=== FILE: B.md ===\nText.\n=== FILE: 1 B - Aside - Y.md ===\n# Y\n\nText.\n=== REPORT ===\nr\n"}},
	})
	if err := extract.Run(context.Background(), p, src, extract.DefaultModel, extract.DefaultEffort, false, folder); err != nil {
		t.Fatal(err)
	}
	got := testfixture.Files(t, filepath.Join(folder.Dir, run.ExtractDir))
	want := map[string]string{
		run.NotesDir + "/0 Tiny.md":             "# 0 Tiny\n\nText.\n",
		run.NotesDir + "/0 Tiny - Aside - X.md": "# 0 Tiny - Aside - X\n\nText.\n",
		run.NotesDir + "/1 B.md":                "# 1 B\nText.\n",
		run.NotesDir + "/1 B - Aside - Y.md":    "# Y\n\nText.\n",
		run.ReportsDir + "/0.md": "No repairs.\n" +
			"\ns2n renamed `4 Tiny.md` to `0 Tiny.md`, and set its H1 to `# 0 Tiny`: a segment's notes are named with its number.\n" +
			"\ns2n renamed `4. Tiny - Aside - X.md` to `0 Tiny - Aside - X.md`, and set its H1 to `# 0 Tiny - Aside - X`: a segment's notes are named with its number.\n",
		run.ReportsDir + "/1.md": "r\n" +
			"\ns2n renamed `B.md` to `1 B.md`, and set its H1 to `# 1 B`: a segment's notes are named with its number.\n",
	}
	for name, w := range want {
		if got[name] != w {
			t.Errorf("%s:\n got %q\nwant %q", name, got[name], w)
		}
	}
	var notes []string
	for name := range got {
		if strings.HasPrefix(name, run.NotesDir+"/") {
			notes = append(notes, name)
		}
	}
	if len(notes) != 4 {
		t.Errorf("notes written: %q, want the four renamed or kept", notes)
	}
}

// A rename that gives a note another note's name is dropped as a doubled
// name.
func TestRun_DropsARenameToANameWrittenTwice(t *testing.T) {
	src, err := source.Parse("T", "---\nsource: \"[[@T]]\"\n---\n\n## A\n\nText.\n")
	if err != nil {
		t.Fatal(err)
	}
	folder, err := run.New(filepath.Join(t.TempDir(), "run"))
	if err != nil {
		t.Fatal(err)
	}
	p := provider.NewReplay(map[string][]provider.Reply{
		extract.Prompt(src, src.Segments[0]): {{Text: "=== FILE: 0 A.md ===\nx\n=== FILE: 4 A.md ===\ny\n=== REPORT ===\nr\n"}},
	})
	if err := extract.Run(context.Background(), p, src, extract.DefaultModel, extract.DefaultEffort, false, folder); err != nil {
		t.Fatal(err)
	}
	want := []dropped.Item{{Segment: 0, Note: "0 A.md", What: "the note", Why: `renamed from "4 A.md", it is a name the reply already wrote`}}
	if got := droppedItems(t, folder); !reflect.DeepEqual(got, want) {
		t.Errorf("dropped %+v, want %+v", got, want)
	}
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
