// Package gate is the shape every check has (ADR 0002 §3). A check is a
// function from parsed notes and the parsed source to a Report. It reads and
// writes no files: the run writes the rendered report into the check's step
// directory, and decides what a failure does. Stage 1's failures don't stop
// the run: their flags feed the repair pass and the run report, and the final
// stage 1 notes drop a broken or untraced statement (ADR 0009). In stages
// 2 and 3 a segment that fails a gate falls back to the notes the stage
// before gave it (package fallback), and the run goes on. Only a fault with
// nothing to fall back to stops it: a segment folder no segment claims, or a
// fallback that can't be stripped.
package gate

import (
	"fmt"
	"strings"
)

// Report is what a check returns.
type Report interface {
	// Failed says whether the check flagged anything.
	Failed() bool
	// String renders the report as the run writes it, as plain text (ADR
	// 0002 §3).
	String() string
}

// Stopped is the error of a gate that stops the run. The run folder keeps the
// gate's report, if it has one, in the step directory left partial, so `s2n
// resume` runs the step again.
type Stopped struct {
	// Gate names the gate, such as "step 2.1's folder check".
	Gate string
	// Segments describe the segments that failed it, such as "segment 3
	// (Arches)", in segment order, or a segment folder no segment claims,
	// such as `folder "99 Extra"`.
	Segments []string
	// Report is the path of the gate's report, or "" if the gate writes
	// none, so its Fix says all the human needs.
	Report string
	// Items are what failed the gate, in the report's order, for the stop's
	// Summary.
	Items []Item
	// Fixes say what the human does about each Kind of Items.
	Fixes map[string]string
	// Fix says what the human does before resuming, for a gate with no
	// Items, such as the folder check, or is "".
	Fix string
}

func (e *Stopped) Error() string {
	if e.Report == "" {
		return fmt.Sprintf("%s failed on %s", e.Gate, strings.Join(e.Segments, ", "))
	}
	return fmt.Sprintf("%s failed on %s; its report is %s", e.Gate, strings.Join(e.Segments, ", "), e.Report)
}

// Item is one thing that failed a gate, as a stop's summary names it, so that
// fixing it needs no report open and no directory search.
type Item struct {
	// Kind names the kind of failure, such as "lost trace". The stop's
	// Fixes say what to do about each kind.
	Kind string
	// Problem says what's wrong, in one plain sentence.
	Problem string
	// Paths are the full paths of the notes at fault. A failure in no one
	// note, such as a trace that a stage 1 note has and no split note has,
	// gives the stage 1 note's path and the segment folder's.
	Paths []string
	// Text is the statement, trace or quote at fault, or "".
	Text string
}

// MaxItems is how many items a stop's summary gives before it points to
// the report for the rest.
const MaxItems = 10

// Summary renders the stop's Items, up to MaxItems of them, each with its
// problem, its paths and the text at fault, then how many more there are,
// then what to do about each kind of item, in the order the kinds first
// appear. It is "" for a stop with no Items.
func (e *Stopped) Summary() string {
	if len(e.Items) == 0 {
		return ""
	}
	var b strings.Builder
	for i, it := range e.Items[:min(MaxItems, len(e.Items))] {
		fmt.Fprintf(&b, "%d. %s\n", i+1, it.Problem)
		for _, p := range it.Paths {
			fmt.Fprintf(&b, "   %s\n", p)
		}
		if text := strings.Trim(it.Text, "\n"); text != "" {
			for line := range strings.Lines(text + "\n") {
				fmt.Fprintf(&b, "   > %s", line)
			}
		}
	}
	if more := len(e.Items) - MaxItems; more > 0 {
		fmt.Fprintf(&b, "\n...and %d more. The report %s gives them all.\n", more, e.Report)
	}
	b.WriteString("\nWhat to do:\n")
	seen := map[string]bool{}
	for _, it := range e.Items {
		if seen[it.Kind] {
			continue
		}
		seen[it.Kind] = true
		fmt.Fprintf(&b, "- For each %s: %s.\n", it.Kind, e.Fixes[it.Kind])
	}
	return b.String()
}
