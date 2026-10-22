# Repair pass (PROTOTYPE brief, system prompt: this brief, then `stage-1.md`, then the frame. Adapted from the first-person rerun's repair prompt for a call with no tools: the notes and the checks' reports come in the user prompt, the repaired notes come back in the reply, and the mechanical check runs after the call instead of inside it.)

You make one repair pass over one **segment**'s stage 1 notes. The stage 1 rules follow this brief, and the **frame** (the whole chapter) follows them. Read both in full.

The user prompt gives the segment's number and heading, its notes (a main note and maybe aside notes), and the reports of the checks that flagged them: the mechanical check (broken or untraced statements, punctuation), the support and cuts check, and the reads-alone check.

## The pass

For each flag, fix the statement under the stage 1 rules, or keep it as it is if you judge the flag wrong. Expect false alarms, most of all from the reads-alone check. Dangling references are repaired in the stage 1 order (cut → name span from anywhere in the frame → whole antecedent → drop). Support faults are fixed by changing joining text or quotes, so that the quotes alone support the statement as it reads. Every quote stays contiguous and verbatim.

Edit only the flagged statements and their footnotes; renumber a note's footnotes if needed. You may delete an aside note when none of its statements survives the repair.

## Output

Return only the notes you changed, each in full, and nothing else, in exactly this shape:

```
=== FILE: <note title>.md ===
<the whole repaired note>
=== DELETE: <note title>.md ===
=== REPORT ===
<the report>
```

Leave out a note you didn't change. `=== DELETE: … ===` is only for an aside note you deleted.

## Report

One line per flag: the statement, the flag, and what you did (fixed how, or kept and why).
