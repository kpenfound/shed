# Change units

- **S.unit.1** (H.unit.1) A unit is identified by a jj change ID: at least 12
  of the letters k to z. Commands accept any prefix that names one unit.
- **S.unit.2** (H.unit.2) A unit is in one state at a time: proposed, sealed,
  implementing, verifying, queued, landed, contested or archived. It opens in
  proposed. Landed and archived units never move again.
- **S.unit.3** (H.unit.3) Shed allows only these moves, and each needs an
  actor and a reason. Proposed to sealed, archived or contested. Sealed to
  implementing. Implementing to verifying. Verifying to queued or back to
  implementing. Queued to landed. Sealed, implementing, verifying or queued
  back to proposed. Contested to proposed or archived. Proposed to landed,
  only for a unit that `shed frame -accept` lands (S.frame.4).
- **S.unit.4** (H.unit.3) Sealing records the seal, the main commit, the
  unit's change ID and the commit that change points to, together with the
  unit's footprint. Archiving records the shelf, rejected or deferred.
- **S.unit.5** (H.unit.4) A move from sealed, implementing, verifying or
  queued back to proposed is a reopen and counts a bounce. A reopen that
  requests an amendment also counts an amendment.
- **S.unit.6** (H.unit.5) Any bounce that leaves a unit's bounce count above
  the operator's bounce threshold, whether a reopen under S.unit.5 or a
  proposal bounced back to its painter, moves it on to contested and leaves a
  notice for the owner. Other units carry on.
- **S.unit.7** (H.unit.6) A unit's footprint may depend only on spec clauses
  that are on main or that the unit itself modifies. Shed refuses a
  dependency on a clause another in-flight unit is adding, naming that unit,
  and on a clause no unit has.
- **S.unit.8** (H.unit.2, H.unit.3) The owner drives units by hand with
  `shed unit open`, `shed unit move` and `shed unit reopen`. By hand a unit
  moves to implementing, verifying or queued. These commands refuse
  sealing, landing, archiving and any move out of contested; the owner
  takes a unit out of contested only with `shed answer` (S.owner.4,
  S.owner.5).
- **S.unit.9** (H.unit.11) A unit leaves contested only by a move with the
  owner as actor, which `shed answer` makes (S.owner.4, S.owner.5,
  S.owner.8, S.shed.17), or by a move to archived with the frame builder as
  actor, which S.frame.7 makes. Shed refuses any other move out of
  contested, whatever its target state, including one by the frame builder
  to proposed, one by the painter, committee, mechanic, wheelbuilder or
  sweeper as the outcome of a session, and one with shed as actor. The
  refusal names the unit and the actor, records no event and leaves the
  unit contested with its bounce count, amendment count and latest move to
  contested unchanged.
