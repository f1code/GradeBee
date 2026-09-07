import type { JobPassage } from '../api-types.gen'
import { PassageChild, PassageUnknown } from '../api-types.gen'

/**
 * The passages a recording holds that reached nobody: an `unknown` block (a
 * pronoun only, or a spoken name matching no one on the roster), or a `child`
 * block the pipeline could not pin to a student. Group passages ride along
 * with whatever note is made and are never a row; `none` is dropped at
 * assembly and never reaches the wire.
 */
export function unattributed(passages: JobPassage[]): JobPassage[] {
  return passages.filter(isUnattributed)
}

export function isUnattributed(p: JobPassage): boolean {
  return (p.kind === PassageUnknown || p.kind === PassageChild) && !p.student
}
