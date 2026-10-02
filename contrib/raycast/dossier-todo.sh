#!/bin/bash

# @raycast.schemaVersion 1
# @raycast.title Dossiers To Do
# @raycast.mode inline
# @raycast.refreshTime 10m
# @raycast.packageName Dossier
# @raycast.icon ✅
# @raycast.description Open dossiers that need you, in every store.

export PATH="$HOME/go/bin:/opt/homebrew/bin:/usr/local/bin:$HOME/.local/bin:$PATH"
stores_json=$(dossier stores --format json)
store_field() { plutil -extract "result.$1.$2" raw -o - - <<<"$stores_json" 2>/dev/null; }
i=0 line=""
while sphere=$(store_field $i sphere); do
  line+="${line:+  ·  }$sphere $(store_field $i todo) to do, $(store_field $i waiting) waiting"
  i=$((i + 1))
done
echo "$line"
