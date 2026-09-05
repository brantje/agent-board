# Planning strategy

Planning strategy is a post-v0.1 execution feature. It must not become a mandatory extra phase for every Run or delay the first complete coding flow.

## Strategies

```text
auto
always
skip
```

### Auto

Default. The Agent evaluates the Issue and current repository state and decides whether a planning phase is useful.

Useful decision signals may include ambiguity, scope, number of subsystems, architecture impact, migrations/refactors, permissions/security risk and whether an existing implementation pattern clearly applies.

### Always plan

Every Run begins with planning/research before implementation.

### Skip planning

Run proceeds directly into implementation.

## Lifecycle

Planning is optional in the Run lifecycle:

```text
Auto -> plan needed -> Planning -> Planned -> Executing
Auto -> direct ---------------------------> Executing
Always -------------> Planning -> Planned -> Executing
Skip ------------------------------------> Executing
```

Exact internal state representation may fit the canonical Run model, but direct execution must remain valid.

## Visibility

For Auto, persist and expose:

- selected strategy
- chosen path: plan first or direct implementation
- short user-visible reason

Do not persist hidden chain-of-thought.

## Configuration precedence

Recommended precedence:

1. explicit Run override, when supported
2. Agent configuration
3. Project/global default, once such defaults exist
4. built-in default: `auto`

Do not invent extra inheritance layers solely for this feature.

## Plan artifact

When planning occurs, retain the generated plan as first-class Run data/Artifact rather than transient model context only.

It should be available for:

- Run detail
- execution history/debugging
- reuse by the same or another Agent where explicitly requested
- future human approval/edit workflows

Human approval before implementation is not required by this feature.

## Interaction with delegation/Squads

Planning does not create another scheduler. A plan may later identify work suitable for delegation, but delegated execution still goes through Agent Board's normal scheduler and Run model.

## Ordering

Implement only after the complete v0.1 real coding-agent flow works reliably. Planning should improve that flow, not become a prerequisite for proving it.
