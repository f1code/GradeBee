import type { JobPassage } from '../api-types.gen'
import { PassageAbsent, PassageChild, PassageUnknown } from '../api-types.gen'

/**
 * The passages a recording holds that reached nobody: an `unknown` block (a
 * pronoun only, or a spoken name matching no one on the roster), or a `child`
 * or `absent` block the pipeline could not pin to a student. Group passages
 * ride along with whatever note is made and are never a row; `none` is dropped
 * at assembly and never reaches the wire.
 *
 * `absent` is here for the same reason `child` is: the teacher spoke a name
 * ("Téo wasn't in today") and it matched nobody listed, so the row is theirs
 * to file. Absence with no name spoken at all is already `unknown` — the
 * extraction guard demotes it.
 */
export function unattributed(passages: JobPassage[]): JobPassage[] {
  return passages.filter(isUnattributed)
}

export function isUnattributed(p: JobPassage): boolean {
  return (p.kind === PassageUnknown || p.kind === PassageChild || p.kind === PassageAbsent) && !p.student
}
