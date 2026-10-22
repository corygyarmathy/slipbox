// Package extract is step 1.1: per segment, the generator writes a main note
// and any aside notes with traces, and a report of its repairs and drops.
//
// By default the generator writes its statements quote-once, briefed by
// briefs.Stage1QuoteOnce (ADR 0006): each quote once, in
// its statement, with the joining text marked. The step then writes the
// traces itself (package quoteonce) before it writes the notes, so the step's
// notes are traced either way. Without quote-once, the generator writes the
// traces, briefed by briefs.Stage1, as trialled before quote-once.
//
// A step 1.1 note's name starts with its segment's number, which every later
// step finds it by. The step sets that number on a note named without it
// and says so in the segment's report. A note it can't name,
// or whose name the segment already wrote, is dropped and listed for the run
// report (ADR 0009).
package extract

import (
	"context"
	"fmt"
	"path"
	"regexp"
	"strconv"
	"strings"

	"github.com/corygyarmathy/slipbox/internal/briefs"
	"github.com/corygyarmathy/slipbox/internal/checker"
	"github.com/corygyarmathy/slipbox/internal/dropped"
	"github.com/corygyarmathy/slipbox/internal/modelstep"
	"github.com/corygyarmathy/slipbox/internal/note"
	"github.com/corygyarmathy/slipbox/internal/provider"
	"github.com/corygyarmathy/slipbox/internal/quoteonce"
	"github.com/corygyarmathy/slipbox/internal/run"
	"github.com/corygyarmathy/slipbox/internal/source"
)

// DefaultModel is stage 1's tier: Opus (Sonnet is the tested fallback).
const DefaultModel = "opus"

// DefaultEffort is the step's effort level: medium (ADR 0004).
// On Ch 4, low cost $0.99 less than medium but its notes left out more of the
// argument: 84% coverage to 88%, and 11 proposed losses to 7.
const DefaultEffort = "medium"

// FirstCallRate is the step's predicted cost in dollars per prompt character
// before it has a finished call (modelstep.Spec.FirstCallRate): step 1.1's
// first call in Ch 4's quote-once run (ADR 0006), at Opus, cost $0.310 for a
// 76,644-character prompt.
const FirstCallRate = 4.05e-6

// Prompt is a segment's user prompt, as trialled.
func Prompt(src *source.Source, seg source.Segment) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Your segment: number %d, title `%s`, section heading `%s`", seg.Number, seg.Title, seg.Heading)
	if seg.PreHeading {
		b.WriteString(" (the text before the first `##` heading).")
	} else {
		b.WriteString(".")
	}
	fmt.Fprintf(&b, " Source file name for traces: `%s`.", src.Name)
	return b.String()
}

// everySegment gives the runner every segment's prompt: extract calls them
// all.
func everySegment(src *source.Source, seg source.Segment) (string, bool) {
	return Prompt(src, seg), true
}

// leadingNumber is a note name's leading number, as a reply may write it:
// `4 ` or, copying a chapter heading's numbering, `4. `.
var leadingNumber = regexp.MustCompile(`^[0-9]+\.? `)

// SegmentName is the name of seg's note named name: name if it starts with
// seg's number and a space, and otherwise name with that number in place of
// its leading number, or in front of it if it has none. A leading number that
// starts seg's title is the title's, and is kept.
func SegmentName(seg source.Segment, name string) string {
	prefix := checker.SegmentPrefix(seg)
	if strings.HasPrefix(name, prefix) {
		return name
	}
	if seg.Title != "" && strings.HasPrefix(name, seg.Title) {
		return prefix + name
	}
	return prefix + name[len(leadingNumber.FindString(name)):]
}

// Brief is step 1.1's brief, and file its file: briefs.Stage1, or
// briefs.Stage1QuoteOnce if quoteOnce is set.
func Brief(quoteOnce bool) (file, text string) {
	if quoteOnce {
		return briefs.Stage1QuoteOnceFile, briefs.Stage1QuoteOnce
	}
	return briefs.Stage1File, briefs.Stage1
}

// Run runs step 1.1 on every segment of src with model at effort, writing the
// step's directory in folder: the notes under `stage-1/`, each segment's
// report as `reports/<n>.md` and its raw reply as `raw/<n>.txt`. The
// directory is complete only if every segment succeeded. With quoteOnce the
// generator writes its statements quote-once, and the notes under `stage-1/`
// are traced by s2n, while `raw/` keeps the replies as written. A note named
// without its segment's number is renamed by SegmentName, its H1 set to the
// new name, and the rename added to the segment's report.
//
// A reply's file that can't be written is dropped, and the step goes on: a
// delete, a name that isn't a note's, or a name the segment already wrote,
// renamed or not. The step lists each in `dropped.json` (package dropped).
func Run(ctx context.Context, p provider.Provider, src *source.Source, model, effort string, quoteOnce bool, folder *run.Folder) error {
	step, err := folder.Begin(run.ExtractDir)
	if err != nil {
		return err
	}
	file, brief := Brief(quoteOnce)
	results, err := modelstep.Run(ctx, p, src, modelstep.Spec{Brief: brief, BriefFile: file, Model: model, Effort: effort, FirstCallRate: FirstCallRate, Prompt: everySegment}, step)
	if err != nil {
		return fmt.Errorf("extract: %w", err)
	}

	var tracer *quoteonce.Tracer
	if quoteOnce {
		tracer = quoteonce.New(src)
	}
	var drops []dropped.Item
	for _, r := range results {
		// A segment's names start with its number, so no two segments'
		// can be the same.
		written := map[string]bool{}
		report := r.Tail
		drop := func(name, why string) {
			drops = append(drops, dropped.Item{Segment: r.Segment.Number, Note: name, What: "the note", Why: why})
		}
		for _, f := range r.Files {
			if f.Delete {
				drop(f.Name, "the reply deletes it, which only a repair call may do")
				continue
			}
			given, err := note.FileName(f.Name)
			if err != nil {
				drop(f.Name, err.Error())
				continue
			}
			name := SegmentName(r.Segment, given)
			if written[name] {
				why := "the reply writes it twice"
				if name != given {
					why = fmt.Sprintf("renamed from %q, it is a name the reply already wrote", given)
				}
				drop(name, why)
				continue
			}
			written[name] = true
			nt := note.Parse(name, f.Body)
			if name != given {
				h1 := strings.TrimSuffix(name, ".md")
				nt.SetH1(h1)
				report = renamed(report, given, name, h1)
			}
			if tracer != nil {
				nt = tracer.Note(r.Segment.Number, nt)
			}
			if err := step.WriteFile(path.Join(run.NotesDir, name), []byte(nt.Render())); err != nil {
				return err
			}
		}
		if err := step.WriteFile(path.Join(run.ReportsDir, strconv.Itoa(r.Segment.Number)+".md"), []byte(report)); err != nil {
			return err
		}
	}
	if err := dropped.Write(step, drops); err != nil {
		return err
	}
	return step.Commit()
}

// renamed adds to report a line saying s2n renamed the note from to to, and
// set its H1 to h1.
func renamed(report, from, to, h1 string) string {
	if report != "" && !strings.HasSuffix(report, "\n") {
		report += "\n"
	}
	return report + fmt.Sprintf("\ns2n renamed `%s` to `%s`, and set its H1 to `# %s`: a segment's notes are named with its number.\n",
		from, to, h1)
}
