package store

const configTemplate = `# dossier store configuration.

[store]
sphere = "{{sphere}}"

[acp]
# ACP server that runs one agent session per dossier, in a herdr tab.
command = ["herdr-acp", "--workspace", "dossiers-{{sphere}}"]
# interaction = "native": questions and permissions stay in the agent's own UI.
meta = { interaction = "native" }

[agent]
# Extra arguments for the agent CLI. dossier always adds --name <id>.
args = []
# Adds --remote-control "<id> · <title>" so the session shows on your phone.
remote_control = true

[prompt]
open = "prompts/open.md"
event = "prompts/event.md"
default_instruction = "Prepare a proposal for the next step, then wait for my decision."

[lifecycle]
# States whose tab closes on its own. The session stays resumable.
close_tab_on = ["done"]

[routing]
# An instruction starting with one of these, followed by a number, is routed
# to that dossier as an event instead of opening a new one ("D-42: ...").
address_prefix = ["D-", "dossier "]

# One block per source connector. command is an argv array, run without a shell;
# a relative path resolves against the store.
#
# [[source]]
# name = "gmail"
# command = ["uv", "run", "connectors/gmail.py"]
# env = { GOOGLE_WORKSPACE_CLI_CONFIG_DIR = "~/.config/gws-{{sphere}}" }
# config = { tasklist = "@default" }
# timeout = "120s"
`

const openPromptTemplate = `You handle dossier {{id}}: {{title}}.

Instruction:
{{instruction}}

{{summary}}
Content: {{files}}

Read the content before you start. Then check whether this affair already has
a dossier, open or closed, with: dossier search "<a few words>". If it does,
tell me which one and wait for my answer.
`

const eventPromptTemplate = `New on dossier {{id}}: {{summary}}

Content: {{files}}

Read it, then tell me what it changes.
`

const indexTemplate = `---
okf_version: "0.2"
---
# Dossiers ({{sphere}})
`

const gitignoreTemplate = `# dossier: version the Markdown, keep heavy files out of git.
/*/context/*
/*/files/*
!/*/context/*.md
!/*/files/*.md
`

const charterTemplate = `# Dossiers ({{sphere}})

Every agent session of this store reads this file. Say here where the memory of
the {{sphere}} sphere lives, how to search it, and what an agent may write there.
`
