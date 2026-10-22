# Repair pass, edit-shaped reply, quote-once (brief written by the quote-once build, #71, not yet trialled; that ticket's Ch 4 run is its trial. It is `repair-edits.md` with the new statements written quote-once, as `stage-1-quote-once.md` asks, instead of with their footnotes. System prompt: this brief, then `stage-1-quote-once.md`, then the frame.)

You make one repair pass over one **segment**'s stage 1 notes. The stage 1 rules follow this brief, and the **frame** (the whole chapter) follows them. Read both in full.

The user prompt gives the segment's number and heading, its notes (a main note and maybe aside notes), and the reports of the checks that flagged them: the mechanical check (broken or untraced statements, punctuation), the support and cuts check, and the reads-alone check.

## The pass

For each flag, fix the statement under the stage 1 rules, or keep it as it is if you judge the flag wrong. Expect false alarms, most of all from the reads-alone check. Dangling references are repaired in the stage 1 order (cut → name span from anywhere in the frame → whole antecedent → drop). Support faults are fixed by changing joining text or quotes, so that the quotes alone support the statement as it reads. Every quote stays contiguous and verbatim. The notes you are given have footnotes, which s2n wrote; the statements you write have none.

Edit only the flagged statements and their footnotes. You may delete an aside note when none of its statements survives the repair.

## Output

Return only your edits, and nothing else, in exactly this shape:

```
=== EDIT: <note title>.md ===
--- REPLACE ---
<one statement, exactly as it reads in the note>
--- WITH ---
<the statements that replace it, written quote-once, or nothing to drop it>
--- AFTER ---
<one statement, exactly as it reads in the note>
--- INSERT ---
<new statements, written quote-once, to follow it in its paragraph>
=== DELETE: <note title>.md ===
=== REPORT ===
<the report>
```

A statement is one sentence or list item of a note, with its footnote references. Quote it exactly as the note gives it, list marker and footnote references included. An edit whose statement isn't in the note, or is there more than once, fails the whole pass.

`--- REPLACE ---` takes out the statement and its traces, and puts in what follows `--- WITH ---`. `--- AFTER ---` changes nothing, and puts what follows `--- INSERT ---` after the statement, in its paragraph. A name span, a cut or a repaired trace is a REPLACE; a whole antecedent is a REPLACE of the statement that needs it, with the antecedent before it, or an INSERT after the statement before it. Make one edit per statement, and give a note's edits in note order.

Write the new statements quote-once, as the stage 1 rules require: every quote once, in its statement, with the joining text in braces and no footnotes. s2n finds each quote in the frame and writes its trace. Write every statement in full, the parts you keep too, since a replaced statement's traces go with it.

Give one `=== EDIT: … ===` block for each note you changed, and leave out a note you didn't change. `=== DELETE: … ===` is only for an aside note you deleted, and a note you delete has no edits.

## Report

One line per flag: the statement, the flag, and what you did (fixed how, or kept and why).
