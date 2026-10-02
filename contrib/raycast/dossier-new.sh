#!/bin/bash

# @raycast.schemaVersion 1
# @raycast.title New Dossier
# @raycast.mode compact
# @raycast.packageName Dossier
# @raycast.icon 📁
# @raycast.argument1 { "type": "text", "placeholder": "Title" }
# @raycast.argument2 { "type": "text", "placeholder": "Instruction", "optional": true }
# @raycast.argument3 { "type": "text", "placeholder": "Store (sphere)", "optional": true }
# @raycast.description Open a dossier and start its agent on the instruction.

export PATH="$HOME/go/bin:/opt/homebrew/bin:/usr/local/bin:$HOME/.local/bin:$PATH"
args=(open --title "$1" --format text)
[ -n "$2" ] && args+=(--instruction "$2")
[ -n "$3" ] && args+=(--store "$3")
dossier "${args[@]}" 2>&1 | head -1
