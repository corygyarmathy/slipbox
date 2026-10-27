# Source-to-Notes

The language of the procedure that turns a source text into refined notes, with every statement traceable to the source and every untouched passage detectable.

## Units

**Stage**:
One of the procedure's four phases: stage 1 extracts traced notes, stage 2 splits them into atomic notes under a hub, stage 3 formats them, and stage 4 integrates them into the vault, which is the user's own work. Each stage is a sequence of numbered steps.
_Avoid_: phase, pass

**Frame**:
The enclosing unit of the source, such as a chapter, that every segment pass reads in full.
_Avoid_: context, window

**Segment**:
A section of a frame, or the untitled text before its first heading; the unit written in one pass, whose notes trace only into it, save name spans.
_Avoid_: chunk, part

**Hub note**:
An ordinary note per segment that links its atomic notes, as a numbered list in argument order, with each aside nested under the note it digresses from; an entry point, not a parent. It carries no statements or traces.
_Avoid_: parent note, index note, MOC

**Atomic note**:
A stage 2 note holding one idea (a proposition, or an argument with its premises) that reads alone, without its hub or neighbours; in practice about one stage 1 paragraph. A split that leaves a note opening on a reference it can't resolve with a name span is the wrong split.
_Avoid_: card, zettel, chunk

**Statement**:
One sentence or list item in an atomic note; the unit that carries traces and that the support check judges.
_Avoid_: claim, line

**Joining text**:
Words in a statement that aren't quoted from the source but connect quoted material; the only new wording allowed besides ordering.
_Avoid_: glue, connective

**Aside**:
A significant digression in a segment that makes a point of its own, beyond the segment's argument; extracted into its own traced note rather than dropped. It is marked as an aside only in its hub (`- Aside: [[...]]`), never in the note itself.
_Avoid_: tangent, side note

## Selection

**Selection rule**:
What a stage 1 note keeps: sentences that state the author's propositions (premises or conclusions) and the arguments connecting them. Illustrations, transitions and examples are dropped unless the example is itself the proposition. Story is illustration; where a kept statement needs a story fact to read correctly, the fact enters that statement through the dangling-reference repairs, never as a note of its own. A segment that restates the chapter (a closing summary) keeps only what earlier segments don't already say.
_Avoid_: summary, filter

**Dangling reference**:
Referring words in a statement whose referent no earlier statement in the same note supplies, typically because selection dropped the antecedent; story facts a statement leans on (who "the night clerk" is, which "we") count too. References that point outside the frame ("the chapter before", "as shown above") count too. No statement in a note may contain one, save an outside-frame reference that no cut can remove, which is kept as the source states it. The author's first-person "I" dangles; generic "we" (people in general) and "you" addressed to the reader don't.
_Avoid_: orphan reference, broken reference

**Cut**:
A stage 1 repair that trims referring words out of a statement's quote, leaving the rest of the sentence as the trace ("We saw that X" becomes "X"). Allowed only when the trimmed words carry no argument content: they only point, they are the author's first-person framing around the claim (stance, tone, how the author knows it), or they tie a general claim to the story (", so I kept my voice low that night"), as in "I half-seriously call this habit a superpower because". Words that link the claim to its argument ("To see why that matters,") carry content and are not cut. Rewording is never a cut.
_Avoid_: trim, elision

**Name span**:
The shortest source span (a noun phrase or a clause) that identifies a referent, quoted in place of or next to a dangling reference and footnoted to its own quote. The only trace that may point outside its segment, anywhere in the frame; and, only to name the author behind a first-person "I" that no cut removes, into the source's bibliographic record.
_Avoid_: name repair, substitution

## Traceability

**Trace**:
A statement's link to a contiguous, verbatim quote from the source, located by section. Traces live only inside the run: they end once stage 2's split is checked, and no output note carries one.
_Avoid_: citation, reference, source link

**Match form**:
The form a quote and the source's text are compared in when the quote is looked for: footnote references, emphasis markers and backslashes dropped, curly quotes straightened, no-break spaces made plain, and whitespace collapsed. A verbatim quote matches the source in match form, so markup, typographic quotes and line wrapping the model changed don't break its trace.
_Avoid_: normalised text, norm

**Quote-once**:
How the model writes statements: each quote once, in its statement, with the joining text in braces and no footnotes. The tool finds each quote in the frame and writes the traces itself.
_Avoid_: single-quote mode, inline quotes

**Source footnote**:
The one footnote an output note keeps, naming the source it came from; it says where the note came from, not which words support which statement, so it is not a trace.
_Avoid_: citation, citekey footnote

**Broken trace**:
A trace whose quote cannot be found in the source, exactly or fuzzily; its statement counts as untraced.
_Avoid_: paraphrase trace, failed citation

**Support check**:
The judgement of whether a trace's quote, by itself, supports its statement as it reads in the note. A name span is the exception: it is judged against the paragraphs around both its quote and its statement's quote, since only context shows it names the right referent.
_Avoid_: faithfulness check, verification

**Coverage check**:
The listing of source sentences that verified traces do not span in full.
_Avoid_: gap analysis, completeness check

**Covered sentence**:
A source sentence spanned entirely by verified traces; a sentence only partly spanned is uncovered.
_Avoid_: cited sentence

**Uncovered list**:
A segment's sentences that the coverage check found not fully covered; the loss proposer's input. A fully covered segment has none.
_Avoid_: gap list, coverage list

## Losses and the report

**Loss**:
An argument-level idea of a segment that none of its notes states, judged by the idea rather than the wording: a lost label for a distinction already stated is not a loss.
_Avoid_: gap, omission

**Loss proposer**:
The independent pass that reads the frame, every stage 1 note and its segment's uncovered list, and proposes that segment's losses with their quotes. It finds losses; it doesn't prove there are no others.
_Avoid_: coverage reviewer

**Kept loss**:
A loss the loss proposer proposed that the run adds to the notes as new traced statements, in a repair call of its own. Every proposed loss is kept: no one chooses among them.
_Avoid_: accepted loss, approved loss

**Remark**:
An item the loss proposer wrote whose quotes cite no line of its segment's uncovered list, such as one quoting "none uncovered". It is never added as a loss; the run report lists it as dropped, once for each run that gave it. A run's text outside any item, such as "No losses proposed.", is not a remark and is listed nowhere.
_Avoid_: comment, non-loss

**Run report**:
What a run writes about itself, one line per item and nothing that needs action: each kept loss added, each statement that still fails after the repair pass, each item dropped and why, each segment that fell back and why, and the asides and the name spans that point outside their segment, counted. It replaced the checkpoint, the human review a run once stopped for after stage 1.
_Avoid_: review packet, checkpoint

**Fallback**:
A segment's notes when a stage 2 or 3 step fails it: the notes the stage before gave that segment, carried on so the run completes. A segment that falls back in stage 2 keeps its stage 1 notes under a hub of its own; one that falls back in stage 3 keeps its notes unformatted.
_Avoid_: drop (a dropped item is gone), skip

## The run

**Brief**:
The system prompt a model step runs with, kept as it was last trialled; the maintainer decides when a change is worth a re-trial.
_Avoid_: prompt, instructions

**Text rule**:
How s2n reads a source's characters, the same in every step: what counts as whitespace, a digit, a letter or a word character, all by Unicode, so a no-break space is whitespace and ē a letter; and text is cut by code point, not byte.
_Avoid_: pystr, string semantics

**Gate**:
A deterministic check that decides whether a step's output goes on as it is. A model's judgement is never a gate.
_Avoid_: validator, guard

**Run folder**:
A run's working files, kept outside the vault: each step's traced notes, reports and replies. A stopped run resumes from it.
_Avoid_: workspace, temp dir

**Usage limit**:
The model plan's cap on use over a period, a session or a week, after which the provider refuses calls until it resets. A call refused for it was never answered: it is waited out, not retried.
_Avoid_: window, quota, capacity

**Staging folder**:
The per-source folder in the vault where a run leaves its finished notes, the name-clash list and the run report, for the user to move into the slip box.
_Avoid_: output folder, inbox

**Name-clash list**:
The finished notes whose names match an existing slip-box note, ignoring case. It renames nothing: replacing, merging or linking is decided in stage 4.
_Avoid_: conflicts, duplicates
