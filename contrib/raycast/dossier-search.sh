#!/bin/bash

# @raycast.schemaVersion 1
# @raycast.title Search Dossiers
# @raycast.mode fullOutput
# @raycast.packageName Dossier
# @raycast.icon 🔎
# @raycast.argument1 { "type": "text", "placeholder": "Words" }
# @raycast.description Search the dossiers of every store, open or closed.

export PATH="$HOME/go/bin:/opt/homebrew/bin:/usr/local/bin:$HOME/.local/bin:$PATH"
stores_json=$(dossier stores --format json)
store_field() { plutil -extract "result.$1.$2" raw -o - - <<<"$stores_json" 2>/dev/null; }
i=0
while root=$(store_field $i root); do
  echo "== $(store_field $i sphere)"
  dossier search "$1" --status all --store "$root" --format text 2>&1
  echo
  i=$((i + 1))
done
