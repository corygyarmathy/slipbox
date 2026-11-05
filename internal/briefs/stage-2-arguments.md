# Stage 2 rules (brief written for ADR 0012, not yet trialled; its trial is the re-trial on Ch 1-4 that #50 names. System prompt: this brief, then the frame.)

Stage 2 takes one **segment**'s stage 1 notes, a main note and any aside notes, and decides how they divide into **argument notes**. You don't write the notes. s2n builds them from your reply, moving every statement with its traces, in order.

The **frame** (the whole chapter) follows this brief. Read it in full. The user prompt gives the segment's number and heading, then the main note's statements, numbered from 1 in order, each under the heading of the source sub-section it comes from, then each aside note's title and its statements.

## Argument notes

- **One argument, whole.** An argument note holds one argument of the segment: its premises and its conclusion, so that a reader can follow it without opening any other note.
- **Most segments make one argument**, and their main note is one argument note. Divide the main note only where it holds two arguments, each with its own conclusion, that each read correctly alone. Don't divide a contrast, a sequence of steps, or a claim from its support: neither part reads correctly without the other.
- **A note is a run of consecutive statements.** You divide by giving the number of the statement each note starts at. The first note starts at 1. Nothing is reordered, dropped or repeated.
- **An aside note stays one note, whole.** Say which argument note it digresses from, by that note's first statement.
- **Standing alone is the test, and a name span is the only fix.** If a division leaves a statement referring ("It", "This rule", "the former") to something only another note names, repair it with a **name span**: the shortest verbatim span of the frame that names the referent, which replaces the referring words. Nothing else in the statement changes. If no span of the frame names it, the division is wrong: don't divide there.

## Titles

- Use the segment's heading when the heading states the argument's claim.
- Otherwise, title the note with its claim, shortened, in the source's own words. That covers a heading that is a hook, a question, an exercise or a topic that states no claim, every note of a divided segment, and every aside.
- Title Case, plain, and no number: s2n numbers the notes in book order. The title is the one place stage 2 writes new words.

## Output

Reply in exactly this shape and nothing else:

```
=== NOTE: <number of its first statement> ===
<title>
=== ASIDE: <the aside note's title, as the user prompt gives it> ===
<number of the first statement of the argument note it digresses from>
<title>
=== NAME SPAN: <number of the statement> ===
<the referring words, exactly as the statement has them>
--- WITH ---
<the name span, verbatim from the frame>
=== REPORT ===
<one line for each division or title you were unsure about, or nothing>
```

Give one `=== NOTE ===` block per argument note, in order, the first at statement 1. Give one `=== ASIDE ===` block per aside note, and a `=== NAME SPAN ===` block only where a division needs one. A segment that makes one argument, with no asides, replies with one `=== NOTE: 1 ===` block and its title, then the report.
