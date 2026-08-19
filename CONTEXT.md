# GradeBee Domain Context

GradeBee is a teacher tool for managing student rosters, turning voice/text observations into notes, and generating report cards. This glossary captures the shared language of the domain.

## Language

### Rostering

**Level**:
A Group-owned entity defining the kind of report card expected (e.g. "Grade 3", "Intermediate"), carrying shared Report Instructions. A row in a `levels` table (`id`, `group_id`, `name` unique within Group, `report_instructions`). Classes reference it by `level_id`.
_Avoid_: Class name, grade (when used loosely)

**Day**:
A mandatory weekday (Monday–Sunday) on every Class — the day of the class's **first** meeting of the week when a Level meets more than once. Mandatory even for a Level with a single weekly meeting. Stored as the full weekday name; abbreviated to three letters in the Class display name.

**Time slot**:
An optional free-text label distinguishing sections taught at the same Level and Day (e.g. "Period 1", "Morning", "14:10").
Can also contain additional days, if the class occurs multiple times a week "14:10 & Fri 16:30"
_Avoid_: Schedule (legacy name), Group (legacy name), Period (ambiguous — a fixed school-wide timetable slot in some school systems)

**Class**:
A concrete teaching group: one chosen shared Level, a mandatory Day, plus an optional free-text Time slot, owned by a teacher. Holds Students. References its Level by `level_id`. Display name is `Level · Ddd` or `Level · Ddd · Time slot` (e.g. "Marcia · Wed", "Marcia · Wed · 14:10").
_Avoid_: Section, course

**Student**:
A learner belonging to exactly one Class.

**Group**:
A shared tenancy boundary (a school or a subgroup within a school) that owns all shared Levels and their instructions. Implemented as a Clerk Organization. Every Level, Class, and Report lives within exactly one Group; there is no personal/ungrouped path.
_Avoid_: Team, org (in UI), school (that is one kind of Group)

**Admin**:
A Group member who creates and edits shared Levels and their instructions (Clerk org `admin` role).

**Teacher**:
A Group member who creates Classes, records observations, and generates Reports (Clerk org `member` role). Sees shared Level instructions read-only; may layer their own transient instructions.
_Avoid_: User (too generic)

### Reports

**Note**:
A per-student observation extracted from a voice or text upload.

**Report**:
An LLM-generated report card for one Student, drawing on their Notes and the Level's Report Instructions.

**Report Instructions**:
Admin-authored, shared guidance attached to a Level that drives a Report's content, structure, and style. **Required** — a Level with no Report Instructions cannot be used to generate Reports (hard gate).

**Review Instructions**:
Deferred. Report review is a future automated LLM self-review pass that runs before the Teacher sees the Report. It will likely reuse the Report Instructions rather than a separate field; a distinct Review Instructions field is added only if evidence later demands it.

**Ad-hoc Instructions**:
A teacher's transient, per-generation free-text guidance layered on top of the shared Level instructions for a single Report run.
_Avoid_: Additional instructions (legacy UI label)

## Relationships

- A **Group** owns many **Levels**; membership + roles (**Admin**/**Teacher**) come from the Clerk Organization.
- A **Level** carries shared **Report Instructions** (Admin-authored). Automated report review is deferred.
- A **Report** is generated from **Notes** + Level **Report Instructions** + **Ad-hoc Instructions**.
- A **Class** references exactly one **Level** (`level_id`), a mandatory **Day**, and carries an optional free-text **Time slot**; the (Level, Day, Time slot) triple is unique per teacher.
- A **Level** belongs to exactly one **Group**.
- A **Student** belongs to exactly one **Class**.

## Flagged ambiguities

- "Group" historically meant the class Time slot; that meaning is now **Time slot**. "Group" now means the tenancy/sharing boundary (a Clerk Organization).
- **Day** is mandatory on every Class, even one that meets only once a week: consistency beats the saved keystroke, and it makes the teacher's week legible. Day means the first occurrence of the week when a Level meets several times.
- Report **review** (automated LLM self-review pass, pre-Teacher) is a deferred enhancement; not modeled with a separate field yet.
- **Scaling risk:** Clerk's plan caps Organizations at 100. The Group=Clerk-Org model is fine for MVP but would need a paid tier or a custom groups table if GradeBee exceeds ~100 Groups. No personal-group-per-user for this reason.
- Existing users (2) migrate into a single Group. Self-serve Group creation/onboarding for brand-new signups is out of MVP scope.
