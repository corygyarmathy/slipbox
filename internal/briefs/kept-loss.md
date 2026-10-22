# Kept-loss repair (brief written by the resume build, #46, not yet trialled; its first recorded run on Ch 4 is its trial. System prompt: this brief, then `stage-1.md`, then the frame. The notes and the kept losses come in the user prompt, and the notes come back in the reply.)

You add the losses a human kept to one **segment**'s stage 1 notes. The stage 1 rules follow this brief, and the **frame** (the whole chapter) follows them. Read both in full.

A loss proposer compared the notes with the frame and proposed ideas from this segment that no note states. A human reviewed the proposals and kept the ones in the user prompt. The user prompt gives the segment's number and heading, its notes (a main note and maybe aside notes) as the human left them, and the kept losses. Each loss has an id and one or two wordings of the idea, each with the source text it was found in (`Quote:`) and what the argument loses without it (`Why:`).

## The pass

Add each kept loss to the notes as new statements under the stage 1 rules: the source's own sentences, with joining text the only new wording, each statement traced to a contiguous verbatim quote from this segment, and every dangling reference repaired in the stage 1 order (cut → name span from anywhere in the frame → whole antecedent). A loss's quotes show where the idea is in the source; trace from the frame, and use as much of the source as the idea needs to read alone. Put each statement where it belongs in the argument: in the main note, or in the aside note whose digression it belongs to.

The human kept every loss here, so don't drop one. If a statement needs more than its own sentence to read alone, add the whole antecedent rather than drop it. If a note already states the idea, add nothing for it and say so in the report.

Change no existing statement, add no note and delete none. Renumber a note's footnotes if needed.

## Output

Return only the notes you changed, each in full, and nothing else, in exactly this shape:

```
=== FILE: <note title>.md ===
<the whole note, with the new statements>
=== REPORT ===
<the report>
```

Leave out a note you didn't change.

## Report

One line per kept loss: its id, the statements you added and the note they're in, and each dangling-reference repair you made; or, if you added nothing, which statement already states it.
