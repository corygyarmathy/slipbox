// Package modelstep is the one model-step runner (ADR 0002 §4). A step
// supplies its brief, its model, its effort level and its per-segment user
// prompt. The runner:
//
//   - builds the system prompt from the brief, the frame and any material
//     every call of the step shares, so all of it is cached
//   - skips the segments the step has no call for
//   - runs the first segment called first, so the cached prefix is written,
//     then the rest in parallel
//   - keeps each raw reply, and its usage, in the step's directory
//   - parses each reply's `=== FILE: … ===` blocks into files
//   - reuses a segment a stopped attempt finished, so `s2n resume` calls
//     only the rest (ADR 0002 §6)
//   - gives each call its predicted cost, for a usagelimit.Gate's check,
//     and makes a call the gate refused for a usage limit again
//
// A call's predicted cost, in dollars at API rates, is its prompt's size in
// characters, system and user prompt together, times a rate: the step's own
// rate once it has a finished call, its cost over its prompt's size, and the
// step's Spec.FirstCallRate before. A call refused for a usage limit
// (*usagelimit.Refused) was never answered: it is logged, and made again
// once the gate lets it start, and it cancels no other call. A limit the
// gate can't wait for (*usagelimit.Unreadable) cancels none either: the calls
// running finish and keep their replies, and it is the step's error. A call
// the gate never started (*usagelimit.NotStarted), as the run was stopped or
// interrupted while it waited, isn't logged: it was neither made nor spent.
//
// A segment is finished when its raw reply is on disk, the step's Check
// accepted it, and its request record matches the request the step would
// make now: the hash of the model, effort level, system prompt and user
// prompt. Every input a reply depends on is in its request, so a segment
// whose notes, flags, brief or frame changed since its reply is called
// again.
//
// The call shape, the brief then the frame as the system prompt, and the
// reply's parsing (Split) are as trialled.
package modelstep

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"sync"
	"unicode/utf8"

	"github.com/corygyarmathy/slipbox/internal/note"
	"github.com/corygyarmathy/slipbox/internal/provider"
	"github.com/corygyarmathy/slipbox/internal/run"
	"github.com/corygyarmathy/slipbox/internal/source"
	"github.com/corygyarmathy/slipbox/internal/usagelimit"
)

// Parallel is how many segment calls run at once after segment 0, as
// trialled.
const Parallel = 4

// Spec is what a step supplies to the runner.
type Spec struct {
	Brief string
	// BriefFile is the file in internal/briefs/ that Brief begins with,
	// which each usage record names.
	BriefFile string
	Model     string
	// Effort is the step's effort level, one of provider.Efforts. A step
	// states it beside its model, so no call inherits it from the settings of
	// whoever runs it.
	Effort string
	// Shared is material every call of the step shares, such as all the
	// stage 1 notes, or "" for none. It goes in the system prompt after the
	// frame, so it is cached with it: `claude -p` writes user prompt content
	// to the cache afresh on every call.
	Shared string
	// Prompt gives seg's user prompt, and false if the step makes no call for
	// it (the loss proposer skips a segment with nothing uncovered).
	Prompt func(src *source.Source, seg source.Segment) (string, bool)
	// Check, if not nil, says whether the step can use a segment's reply, and
	// why not. A reply it rejects is kept in `raw/` but isn't finished, so
	// `s2n resume` calls its segment again, and the step fails once the
	// other calls have finished. It must depend only on the reply and what
	// the segment's request holds, since a reused reply isn't checked again.
	Check func(Result) error
	// FirstCallRate is the step's predicted cost in dollars per prompt
	// character before it has a finished call. Each step measures its own on
	// Ch 4's recorded runs: stage 1's steps on the quote-once run (ADR 0006),
	// at their tiers, and step 2.1 on the stage 2 tier trial (ADR 0005),
	// which ran it at each tier from that run's final stage 1 notes. A first call writes the cache, so its rate is
	// higher than the step's later calls'.
	FirstCallRate float64
	// Run numbers this run of a step that runs more than once (the loss
	// proposer runs twice), from 1, or is 0 for a step run once. A numbered
	// run keeps its raw replies under `run-<k>/` and names itself in each
	// usage record, so one run's replies don't overwrite another's.
	Run int
}

// Result is one segment's reply.
type Result struct {
	Segment source.Segment
	// Text is the reply as received, for a step whose reply is not in the
	// `=== FILE: … ===` shape, such as a checker's report.
	Text  string
	Files []File
	// Tail is the trailing `=== REPORT ===` or `=== MANIFEST ===` block.
	Tail string
}

// SystemPrompt is the brief, a blank line, then the frame between
// `=== FRAME: <source name>.md ===` and `=== END FRAME ===` lines, then, if
// shared is not "", a blank line and shared as given.
func SystemPrompt(brief string, src *source.Source, shared string) string {
	system := brief + "\n\n=== FRAME: " + src.Name + ".md ===\n" + src.Frame + "\n=== END FRAME ===\n"
	if shared != "" {
		system += "\n" + shared
	}
	return system
}

// Run calls the model once per segment of src that spec has a prompt for,
// writing each raw reply to `raw/<n>.txt`, or `run-<k>/raw/<n>.txt` for a
// numbered run, and each call's usage to `usage.jsonl` in step. A reply
// spec.Check accepts is finished: its request's hash goes to
// `requests/<n>.sha256` beside `raw/`. A segment a stopped attempt finished
// with the same request is reused from step's directory, with no call.
// Results are in segment order, one per segment with a prompt. The first
// call that fails cancels the calls still running; a reply Check rejects
// doesn't, and the first rejection is the error once every call has ended.
// Nor does a usage limit: a refused call is made again, and one the gate
// can't wait for is the error once every call has ended.
// An effort level that isn't one of provider.Efforts is an error before any
// call.
func Run(ctx context.Context, p provider.Provider, src *source.Source, spec Spec, step *run.Step) ([]Result, error) {
	if !slices.Contains(provider.Efforts, spec.Effort) {
		return nil, fmt.Errorf("effort level %q is not one of %v", spec.Effort, provider.Efforts)
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	system := SystemPrompt(spec.Brief, src, spec.Shared)

	type call struct {
		seg source.Segment
		req provider.Request
		key string
	}
	var (
		calls   []call
		results []Result
		pending []int
	)
	for _, seg := range src.Segments {
		user, ok := spec.Prompt(src, seg)
		if !ok {
			continue
		}
		req := provider.Request{Model: spec.Model, Effort: spec.Effort, System: system, User: user}
		key, err := requestKey(req)
		if err != nil {
			return nil, err
		}
		r, ok, err := reuse(step, spec.Run, seg, key)
		if err != nil {
			return nil, err
		}
		if !ok {
			pending = append(pending, len(calls))
		}
		calls = append(calls, call{seg, req, key})
		results = append(results, r)
	}
	if len(pending) == 0 {
		return results, nil
	}

	var (
		mu        sync.Mutex
		rejection error
		// spentUSD and spentChars are the step's finished calls' cost and
		// prompt size, for its rate.
		spentUSD   float64
		spentChars int
	)
	predict := func(req provider.Request) float64 {
		mu.Lock()
		defer mu.Unlock()
		rate := spec.FirstCallRate
		if spentChars > 0 {
			rate = spentUSD / float64(spentChars)
		}
		return rate * float64(promptChars(req))
	}
	callSegment := func(i int) error {
		seg := calls[i].seg
		n := strconv.Itoa(seg.Number)
		requestFile := path.Join(RunDir(spec.Run), run.RequestsDir, n+".sha256")
		// The raw reply about to be written is no longer the one any record
		// on disk describes.
		if err := step.Remove(requestFile); err != nil {
			return err
		}
		var reply provider.Reply
		for {
			var err error
			reply, err = p.Call(usagelimit.WithCost(ctx, predict(calls[i].req)), calls[i].req)
			// Every call made is logged, a failed one too: it may have been
			// spent, and its result says why it failed. One the gate never
			// started wasn't made, and isn't.
			var notStarted *usagelimit.NotStarted
			if errors.As(err, &notStarted) {
				return fmt.Errorf("%s: %w", seg, err)
			}
			u, result := reply.Usage, ""
			if err != nil {
				u.IsError, result = true, reply.Text
			}
			if err := logUsage(step, seg, spec, u, result); err != nil {
				return err
			}
			var refused *usagelimit.Refused
			if errors.As(err, &refused) {
				continue
			}
			if err != nil {
				return fmt.Errorf("%s: %w", seg, err)
			}
			break
		}
		mu.Lock()
		spentUSD += reply.Usage.CostUSD
		spentChars += promptChars(calls[i].req)
		mu.Unlock()
		if err := step.WriteFile(path.Join(RunDir(spec.Run), run.RawDir, n+".txt"), []byte(reply.Text)); err != nil {
			return err
		}
		r := parse(seg, reply.Text)
		if spec.Check != nil {
			if err := spec.Check(r); err != nil {
				mu.Lock()
				if rejection == nil {
					rejection = err
				}
				mu.Unlock()
				return nil
			}
		}
		results[i] = r
		return step.WriteFile(requestFile, []byte(calls[i].key+"\n"))
	}

	if err := callSegment(pending[0]); err != nil {
		return nil, err
	}

	var (
		wg       sync.WaitGroup
		firstErr error
		sem      = make(chan struct{}, Parallel)
	)
	for _, i := range pending[1:] {
		wg.Add(1)
		go func() {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				return
			}
			defer func() { <-sem }()
			if err := callSegment(i); err != nil {
				var unreadable *usagelimit.Unreadable
				mu.Lock()
				if firstErr == nil {
					firstErr = err
					if !errors.As(err, &unreadable) {
						cancel()
					}
				}
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if firstErr != nil {
		return nil, firstErr
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if rejection != nil {
		return nil, rejection
	}
	return results, nil
}

// promptChars is req's prompt's size in characters, for its predicted cost.
func promptChars(req provider.Request) int {
	return utf8.RuneCountInString(req.System) + utf8.RuneCountInString(req.User)
}

// parse gives seg's result for its reply text.
func parse(seg source.Segment, text string) Result {
	files, tail := Split(text)
	return Result{Segment: seg, Text: text, Files: files, Tail: tail}
}

// requestKey is the hex SHA-256 of req, every input a reply depends on.
func requestKey(req provider.Request) (string, error) {
	b, err := json.Marshal(req)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}

// reuse gives seg's result from the raw reply in step's directory, and true,
// if a stopped attempt finished seg with the request whose hash is key.
func reuse(step *run.Step, k int, seg source.Segment, key string) (Result, bool, error) {
	n := strconv.Itoa(seg.Number)
	dir := filepath.Join(step.Dir, filepath.FromSlash(RunDir(k)))
	got, err := os.ReadFile(filepath.Join(dir, run.RequestsDir, n+".sha256"))
	if errors.Is(err, fs.ErrNotExist) {
		return Result{}, false, nil
	}
	if err != nil {
		return Result{}, false, err
	}
	if string(got) != key+"\n" {
		return Result{}, false, nil
	}
	text, err := os.ReadFile(filepath.Join(dir, run.RawDir, n+".txt"))
	if errors.Is(err, fs.ErrNotExist) {
		return Result{}, false, nil
	}
	if err != nil {
		return Result{}, false, err
	}
	return parse(seg, string(text)), true, nil
}

// Fault is a reply that can't be written as its segment's notes: the
// segment's failure, not the run's, which a stage 2 or 3 step turns into a
// fallback (package fallback).
type Fault struct {
	Segment source.Segment
	// Why says what is wrong with the reply.
	Why string
}

func (e *Fault) Error() string {
	return fmt.Sprintf("%s: the reply %s", e.Segment, e.Why)
}

// Notes gives r's files as whole notes, each under its file name, in the
// reply's order. A delete or edits, a name that isn't a plain note name, or a
// name given twice is a *Fault.
func Notes(r Result) ([]File, error) {
	var out []File
	seen := map[string]bool{}
	for _, f := range r.Files {
		if f.Delete || f.Edit {
			return nil, &Fault{r.Segment, fmt.Sprintf("gave %q as a delete or edits, not a whole note", f.Name)}
		}
		name, err := note.FileName(f.Name)
		if err != nil {
			return nil, &Fault{r.Segment, fmt.Sprintf("named a note %q, not a plain file name", f.Name)}
		}
		if seen[name] {
			return nil, &Fault{r.Segment, fmt.Sprintf("wrote %q twice", name)}
		}
		seen[name] = true
		out = append(out, File{Name: name, Body: f.Body})
	}
	return out, nil
}

// WriteNotes writes notes, as Notes gives them, into dir in step.
func WriteNotes(step *run.Step, dir string, notes []File) error {
	for _, f := range notes {
		if err := step.WriteFile(path.Join(dir, f.Name), []byte(f.Body)); err != nil {
			return err
		}
	}
	return nil
}

// RunDir is the directory, inside its step's, of run k's files: `run-<k>`,
// or "" for a step run once (k is 0).
func RunDir(k int) string {
	if k == 0 {
		return ""
	}
	return run.RunPrefix + strconv.Itoa(k)
}

// usageRecord is one line of a step's usage log: the step, its run if it is
// numbered, the segment, the model asked for, its effort level and its
// brief's file, then what the call consumed, with the usage limits' reading
// after it, and, for a failed call, what claude replied.
type usageRecord struct {
	Step    string `json:"step"`
	Run     int    `json:"run,omitempty"`
	Segment int    `json:"segment"`
	Model   string `json:"model"`
	Effort  string `json:"effort"`
	Brief   string `json:"brief"`
	provider.Usage
	Result string `json:"result,omitempty"`
}

// Total is what a run's model calls consumed.
type Total struct {
	Calls   int
	CostUSD float64
	// Seconds is the calls' time added up, more than the time the run
	// took when calls ran in parallel.
	Seconds float64
}

// Totals adds up every usage log in the run folder dir.
func Totals(dir string) (Total, error) {
	var t Total
	err := walkUsage(dir, func(u provider.Usage) {
		t.Calls++
		t.CostUSD += u.CostUSD
		t.Seconds += u.Seconds
	})
	return t, err
}

// LatestReading gives the latest usage limits' reading in every usage log in
// the run folder dir, or nil if none holds one, so that a resumed run starts
// from what the stopped one last read.
func LatestReading(dir string) (*provider.Reading, error) {
	var latest *provider.Reading
	err := walkUsage(dir, func(u provider.Usage) {
		if u.Reading != nil && (latest == nil || u.Reading.At.After(latest.At)) {
			latest = u.Reading
		}
	})
	return latest, err
}

// walkUsage calls f with each record of every usage log in the run folder
// dir.
func walkUsage(dir string, f func(provider.Usage)) error {
	return filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || d.Name() != run.UsageFile {
			return err
		}
		file, err := os.Open(p)
		if err != nil {
			return err
		}
		defer file.Close()
		for dec := json.NewDecoder(file); ; {
			var u provider.Usage
			if err := dec.Decode(&u); errors.Is(err, io.EOF) {
				return nil
			} else if err != nil {
				return fmt.Errorf("%s: %w", p, err)
			}
			f(u)
		}
	})
}

// logUsage appends one call's record to step's usage log, with result, what
// a failed call replied, or "".
func logUsage(step *run.Step, seg source.Segment, spec Spec, u provider.Usage, result string) error {
	line, err := json.Marshal(usageRecord{Step: step.Name, Run: spec.Run, Segment: seg.Number, Model: spec.Model, Effort: spec.Effort, Brief: spec.BriefFile, Usage: u, Result: result})
	if err != nil {
		return err
	}
	return step.AppendFile(run.UsageFile, append(line, '\n'))
}
