---
name: dossier
description: Track an affair as a dossier with its own agent session. Use when the user signals something to follow up (an email, a memo, a task), when a dossier must wait for a third party, be closed, reopened or merged, or when you work inside a dossier session (DOSSIER_ID is set).
---

# dossier

`dossier` turns each signal into a dossier: a directory, a status
(`open`, `waiting`, `done`, `merged`) and one agent session in a herdr tab.

Two doors, nothing else:

- `dossier schema` → categories; `dossier schema <category> <action>` → the
  exact parameters, examples and effects. Copy an example from the leaf.
- `dossier <action> --help` → syntax reminder.

Every action answers `{"ok": true, "result": …}` or
`{"ok": false, "error": {…}}`; the error message shows the canonical call.

Inside a dossier session, `DOSSIER_ID` names your dossier: actions that take
an optional id act on it by default.
