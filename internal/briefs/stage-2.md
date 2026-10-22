# Stage 2 rules (PROTOTYPE brief, given verbatim as the system prompt)

Stage 2 takes one segment's stage 1 notes (the main note plus any aside notes) and **splits** them into atomic notes, then writes one **hub note** for the segment.

## Atomic notes

- **One idea per note**: one proposition, or one argument with the premises that support it. A reader should be able to understand the note at one read, without opening the hub or any sibling. In practice that's about one stage 1 paragraph (around 70 words, rarely over 200).
- **Every statement moves, none is written.** Each statement (a sentence or list item) of the stage 1 notes lands in exactly one atomic note, together with its footnote(s). The quotes in the footnotes are never changed. No statement is dropped and none is duplicated.
- **Standing alone is the test, and merging is the fix.** If a statement would open a note on a reference ("It", "This rule", "he", "the former") whose referent stayed in another note, you may repair it only by replacing the referring words with a **name the source uses** for the referent, added as a **name span**: the shortest source span naming it, with its own footnote in the trace format. Change nothing else. If no source name repairs it, the split is wrong: keep the statement with the note it refers to. List every edited statement in the manifest.
- **Order inside a note** follows the stage 1 order.
- **Title** = H1. Use the source's own words for the idea where possible (its term, or its proposition shortened), in Title Case. The title is the one place stage 2 writes new words, so keep it plain.
- **Format**: frontmatter `aliases: []` and `tags: []`, H1, body, footnote definitions at the bottom. Footnotes are renumbered from 1 in each note. Atomic notes carry **no** link lists. Add no list the source doesn't give; a list split across notes keeps its numbering only if the source numbers it.
- **Asides** are split under the same rules, but never mixed with main-note statements.

## Hub note

- Titled after the segment's section heading. There's one per segment.
- Frontmatter as above, then the H1, then a numbered list of `[[<atomic note title>]]` links in argument order. Each aside note is nested under the note it digresses from as `   - Aside: [[<title>]]`. No statements, no traces. This is the only place an aside is marked.

## Output

Reply with every file, then the manifest, in exactly this shape and nothing else:

```
=== FILE: <note title>.md ===
<the note>
=== FILE: <hub title>.md ===
<the hub>
=== MANIFEST ===
{"segment": "<section heading>",
 "notes": [{"title": "...", "aside": false, "words": 0}],
 "edited_statements": [{"note": "...", "before": "...", "after": "...", "why": "..."}],
 "hard_calls": ["any split you were unsure about, one line each"]}
```
