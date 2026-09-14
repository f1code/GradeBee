# Group statements reach the whole roster

**Status:** accepted

## Context & Decision

A group passage ("all the kids did well with the animals") used to join only the Notes of
children the recording named. A child the teacher said nothing about got nothing: silence
read as absence.

We **reverse that: silence is presence.** A group passage joins the Note of every Student on
the pinned Class's roster, except children the teacher named as absent. Absence must be
spoken, and it is a passage kind (`absent`), extracted like `child`.

One exception protects the wrong-class recovery path: **if the recording spoke names and none
of them were on the roster, group passages reach nobody.** That recording was read against the
wrong Class; fanning out would write Notes for that whole roster and close the class picker.
An `absent` passage counts as a name that matched.

The model only spots absence. The fan-out is Go (`assemblePassages`), pure and unit-testable
with no LLM call.

## Considered Options

- **Keep reading silence as absence.** Rejected: a teacher's "everyone did well" is about the
  hour, and a child the teacher didn't single out was there for it.
- **Absence as a top-level list** in the extraction schema. Rejected: a passage kind keeps the
  teacher's words on the child's Note ("Théo was absent today") and reuses the `child` shape.
- **Absence as a Go keyword scan.** Rejected: teachers phrase it too many ways ("didn't make it
  in", "no Lina today"); the model handles phrasing, Go handles rules.
- **Suppress when any spoken name fails to match.** Rejected: one mangled name would strip the
  group text from every correctly named child. Suppression needs *zero* matches.
- **Mark group-only Notes** (source value, column, or flag on `NoteLink`). Deferred: no
  consumer. Revisit when a Report reads badly for a child covered only by group remarks, or a
  teacher asks why a child has a Note they never spoke about.

## Consequences

- A group-only recording writes a Note for every roster child. Before, it wrote none.
- A teacher who forgets a child produces a group-text Note for them. Accepted.
- Two recordings in one day both fan out; a child named in neither gets two group-only Notes.
- Notes per recording rises to roster size; anything reading it as a quality signal sees a
  step change.
- If the whole roster is named absent, the group text reaches nobody and is dropped. Accepted:
  rescuing it needs a new assign contract for a self-contradictory recording.
- The eval twin (`evals/scoring/assemble.js`) must apply the same fan-out, absent exclusion and
  suppression, or the eval grades a contract that no longer ships.
