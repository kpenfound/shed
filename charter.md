# Charter

The purpose and boundaries of shed. Only the owner changes this document (C17).
Every clause is cited by its ID.

## Purpose

- **C1** Shed is a spec-driven software factory. Agents propose, debate,
  implement, verify and land changes to one git repository, and the owner
  rarely has to step in.
- **C2** The unit of work is a change to the spec. A change lands on main only
  when the code does what the changed spec says, so main always matches its
  spec.
- **C3** Debate is the centre of the factory. Any stage may send a change back
  to debate, and every debate terminates.
- **C4** Shed builds on busybees/core for agent sessions, sandboxing, tool
  hosting, review and operational building blocks. A general gap in core is
  fixed in core. Only shed-specific code lives in shed.
- **C5** How autonomous shed is on a project comes from that project's charter
  and operator configuration. One binary serves every posture.

## Non-goals

- **C6** Shed is not an issue tracker, project planner or hosted service. It
  needs a git remote and nothing else from outside the machine it runs on.
- **C7** Shed is not an agent runtime or a context engine. Running models
  belongs to busybees/core and long-lived memory belongs to a separate context
  provider.
- **C8** Shed does not schedule by dates, estimates or sprints. Distance from
  the spec orders work and footprint sizes it.
- **C9** Shed serves one owner per project. It is not multi-tenant.

## Boundaries

- **C10** Workflow states, transitions, footprints, seals, queue order and
  reconciliation are deterministic Go. No model call decides them.
- **C11** Agents never run version control. Shed performs every VCS operation
  and gives sessions a plain directory of files.
- **C12** Main advances only when a whole change unit lands as one commit,
  fast-forward, under the single landing identity. Nothing edits the spec on
  main directly.
- **C13** Git holds content. Workflow state lives in shed's tracker, never in
  refs, labels or hosted-platform metadata.
- **C14** Certainty is a count of dissent. Shed never acts on an agent's
  self-reported confidence.
- **C15** The owner blocks the factory only by leaving a charter amendment
  unratified. Everything else that waits on the owner is routed around.
- **C16** Tests never call a real model, agent binary or remote service.

## Amendment rule

- **C17** Only the owner amends this charter. Agents may draft an amendment and
  debate its wording. The owner ratifies or declines it, and shed archives a
  declined amendment.
- **C18** Clause IDs are permanent. Removing a clause retires its ID, and no ID
  is reused.
- **C19** Operator settings such as budgets, concurrency, models and round caps
  never belong in this charter.
