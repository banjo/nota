#!/bin/sh
# Writes a small demo vault (English, neutral content) for screenshots and
# manual testing: scripts/demo-vault.sh /tmp/nota-demo
set -eu
dir=${1:?usage: demo-vault.sh DIR}
rm -rf "$dir"
mkdir -p "$dir/Inbox" "$dir/Projects/Nota" "$dir/Projects/Chess club" "$dir/Meetings" "$dir/.nota/views"

note() { # note PATH (content on stdin)
  cat >"$dir/$1"
}

note "Welcome.md" <<'EOF'
---
tags: [welcome]
status: done
created: 2026-11-18T09:00:00+01:00
updated: 2026-11-22T08:40:00+01:00
---
# Welcome to Nota

This is a demo vault: plain Markdown files with YAML at the top, written by scripts/demo-vault.sh.

A few things to try:

- Click **Standup, Friday** in the tree. Its tags and status are frontmatter, edited in the panel above the note.
- Press ⌘K (⌃K elsewhere) and search for *sync*. There is a note about [[Sync protocol]].
- Open the **Inbox**. Three notes are waiting; Nota suggests where each one belongs.
- Type here. The editor is CodeMirror, with live preview.
EOF

note "Getting around.md" <<'EOF'
---
tags: [help]
status: done
created: 2026-11-18T09:10:00+01:00
updated: 2026-11-21T17:02:00+01:00
---
# Getting around

The keys worth knowing, all of them the app's:

- ⌘K opens the command palette: search every note, or run a command.
- ⌘N makes a note in the folder you are looking at.
- ⌘⇧M moves the note on screen to another folder.
- ⌘⇧I goes to the inbox.
- ⌘, opens Settings, where the appearance lives.

Links are wikilinks: [[Standup, Friday]] is a note, and Nota lists the notes that link back at the bottom of the page.

Tags can also be written inline, like #welcome, and show up in the sidebar with the rest.
EOF

note "Inbox/Plumber, kitchen tap.md" <<'EOF'
The kitchen tap drips at night. Ask about the washer size before booking a visit, and whether the landlord pays.
EOF

note "Inbox/Gift ideas.md" <<'EOF'
Mum: a good chef's knife, or the bread book she keeps borrowing.

Leo: the climbing guide to Font. The new edition.
EOF

note "Inbox/Read later, WAL mode.md" <<'EOF'
SQLite in WAL mode: what changes for readers and writers, and why checkpoints matter. Add to the [[Search ranking]] note when read.
EOF

note "Projects/Standup, Friday.md" <<'EOF'
---
tags: [standup, nota]
status: draft
created: 2026-11-20T09:12:00+01:00
updated: 2026-11-22T09:31:00+01:00
---
# Standup, Friday

Shipped the [[Sync protocol]] page. Next: the ranking pass in [[Search ranking]].

## Notes

- 10k notes reindex in about a second on the laptop.
- A search answers in under 20 ms, snippets included.
- The index is disposable: delete .nota/index.db and it comes back.

## To do

- [x] Frontmatter keeps unknown keys
- [ ] Rank titles above body text
- [ ] A keyboard shortcut for backlinks
EOF

note "Projects/Nota/Sync protocol.md" <<'EOF'
---
tags: [nota, sync]
status: done
created: 2026-11-17T14:20:00+01:00
updated: 2026-11-20T18:05:00+01:00
---
# Sync protocol

File-level, with a three-way merge. The server keeps an optimistic version per note and answers 409 when the base is stale, so a client merges instead of overwriting.

The rules, briefly:

1. A pull goes by cursor; a server nudge is only a hint to pull.
2. Edits to different lines merge. The same lines become a conflict copy next to the original.
3. Deletes are tombstones, and an edit beats a delete.

Plan for the week in [[Standup, Friday]].
EOF

note "Projects/Nota/Search ranking.md" <<'EOF'
---
tags: [nota, search]
status: draft
created: 2026-11-19T11:45:00+01:00
updated: 2026-11-22T07:50:00+01:00
---
# Search ranking

FTS5 with bm25, plus a nudge for notes whose title matches: a title hit is worth more than a body hit. Snippets come from the same query, so a result always shows why it matched.

Open questions:

- Do tags belong in the rank, or only in the filter?
- How much should a recent edit count?

Links to [[Sync protocol]] because both need a stable hash per note.
EOF

note "Projects/Chess club/Ratings — Glicko-2.md" <<'EOF'
---
tags: [chess]
status: done
created: 2026-11-12T10:00:00+01:00
updated: 2026-11-16T22:15:00+01:00
---
# Ratings

Glicko-2 per pool, stored next to the games, recomputed nightly. The bench numbers live in the repo, not here.
EOF

note "Meetings/Planning, November.md" <<'EOF'
---
tags: [planning]
status: draft
created: 2026-11-21T10:00:00+01:00
updated: 2026-11-21T10:42:00+01:00
---
# Planning, November

Decisions from the meeting:

- Nota ships the desktop app with v1; the CLI is out first.
- Sync stays self-hosted.
- The landing page shows the app itself, not a video.

Follow-ups in [[Standup, Friday]].
EOF

note ".nota/views/drafts.yaml" <<'EOF'
name: Drafts
icon: pen
where: { status: draft }
sort: updated desc
EOF

note ".nota/views/sync.yaml" <<'EOF'
name: Sync
icon: flag
tag: sync
sort: updated desc
EOF

echo "demo vault in $dir"
