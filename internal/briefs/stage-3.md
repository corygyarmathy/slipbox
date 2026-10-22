# Stage 3 rules (PROTOTYPE brief, given verbatim as the system prompt; the stage-3-trial brief with output as the reply)

Stage 3 takes one segment's stage 2 output (atomic notes plus the hub note) and **formats** it under the formatting convention below. Edit the files in place.

## The one constraint: markup only

Stage 3 adds or removes **markup**. It never changes a character of statement text. That means no rewording, no recasing, no punctuation changes, no reordering of statements across notes, and no splitting or merging of notes. If the convention seems to call for a text change (for example recasing, or turning a run-on sentence into a list by dropping "; or,"), don't make it. Record it in `hard_calls` instead.

Markup that you may add or remove:

- `_x_` and `**x**` around existing words.
- List markers (`- `, `1. `) and the line breaks and blank lines around them, as long as the words and punctuation stay the same and stay in order.
- A callout: `> [!Info] <title>` or `> [!Example]`, wrapping existing statements, each wrapped line prefixed `> `. A callout title must be the source's own name for the rule, copied verbatim from the source (e.g. `RULE 6`), never new words.
- `## ` / `### ` headings, only with a heading text the source itself uses verbatim.
- `>` blockquote, for a passage whose exact wording is the point.
- Wikilinks: leave the existing ones as they are. Add none.

Never touch: the frontmatter, the H1, the file name, footnote references `[^n]` (keep each one attached to its statement and in place after punctuation), footnote definitions (byte-for-byte), or the hub's links and their order.

A mechanical check will strip markup from your output and diff it against stage 2. Any text change will show up. Stages 1 and 2 never add a list the source doesn't give, so don't turn running statements into a list unless the source enumerates them or states a count.

## The formatting convention

Each element has one meaning, and formatting adds no meaning the source doesn't give. Check the source for what it actually emphasises, numbers, lists or names as a rule.

| Element | Means | Not used for |
| --- | --- | --- |
| `_x_` | emphasis the source makes; titles of works | wrapping links, added emphasis |
| `**x**` | the term an item or sentence is about (list lead term, term at its definition) | headings, key sentences, emphasis |
| H2 / H3 | section of one note / subdivision of an H2, no skipped levels | numbering unless the source numbers it; footnotes inside |
| Numbered list | order, count or numbering the source states | unordered sets |
| Bullet list | any other set, after a colon lead-in and a blank line | |
| `[!Info] <source's name>` | a rule, definition or principle the source sets out as one | |
| `[!Example]` | an example or scenario the source gives | |
| `[!Note]` | the user's own voice (stage 4), after the H1 | procedure output: never use it |
| `>` | a passage whose exact wording is the point: poetry, epigraph, sample sentence | asides, "See further" |
| Inline "…" + attribution | a short quote from someone other than the source author | |
| Wikilink | inline on the concept phrase, aliased, first mention only | link lists in atomic notes |
| Table | only where the source has one | |

Callout types are Title Case. Not used in output: `[!Quote]`, `[!Tip]`, `[!Warning]`, highlights, horizontal rules, tasks, embeds, `%% %%` comments, and status markers.

The skeleton is: frontmatter (`aliases: []`, `tags: []`), a blank line, the H1, a blank line, the body, a blank line, footnote definitions. Fix blank lines to match the skeleton if they don't.

Most notes will need little or nothing. "No change" is a correct outcome; don't add formatting to have done something.

## Output

Reply with **every** note, changed or not, then the manifest, in exactly this shape and nothing else:

```
=== FILE: <note title>.md ===
<the note>
=== MANIFEST ===
{"segment": "<section heading>",
 "changes": [{"note": "<title>", "element": "_x_ | **x** | list | [!Info] | [!Example] | > | heading | skeleton", "what": "one line", "judgement": "mechanical | judgement", "why": "one line"}],
 "unchanged_notes": ["<title>"],
 "hard_calls": ["one line each"]}
```

`judgement` is **mechanical** if a script could have done it from the source alone (e.g. the source italicises the word), and **judgement** if you had to decide what the text means.
