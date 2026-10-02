# Raycast Script Commands

Raycast → Settings → Extensions → Script Commands → Add Directories → this directory.

| Command | Does |
|---|---|
| New Dossier | `dossier open` with a title, an instruction and a store (sphere name, optional) |
| Search Dossiers | `dossier search` in every store |
| Open Dossier | `dossier attach`: focuses the dossier's agent tab; set `DOSSIER_TERMINAL_APP` to bring the terminal to the front |
| Dossiers To Do | inline count of what needs you and what waits, refreshed every 10 minutes |

The scripts expect `dossier` in `~/go/bin`, `/opt/homebrew/bin`, `/usr/local/bin` or `~/.local/bin`.
