#!/bin/sh
# Writes a realistic demo vault (Swedish notes) for screenshots and
# manual testing: scripts/demo-vault.sh /tmp/nota-demo
set -eu
dir=${1:?usage: demo-vault.sh DIR}
rm -rf "$dir"
mkdir -p "$dir/Inbox" "$dir/Projekt/Nota" "$dir/Projekt/Schackklubb" "$dir/Fotboll" "$dir/Hem" "$dir/Recept" "$dir/.nota/views"

note() { # note PATH (content on stdin)
  cat >"$dir/$1"
}

note "Projekt/Nota/Sync-protokoll.md" <<'EOF'
---
tags: [nota, sync]
status: draft
created: 2026-11-18T10:12:00+01:00
updated: 2026-11-22T10:42:00+01:00
---
# Sync-protokoll

Filnivå-sync med 3-way merge. [[CLI-kommandon]] ska kunna trigga en sync manuellt, och allt annat sker i bakgrunden.

## Flöde

1. Watchern märker en ändring och jämför `local_hash` mot `base_hash`
2. `PUT /files/{path}` med `base_version`
3. Vid 409: hämta servern, merga mot base, pusha igen
4. Krock på samma rader blir en **konfliktkopia**

> Filen på disk är alltid sanningen. Servern vet inte vad markdown är.

## Att göra

- [x] Välj diff-lib (sergi/go-diff)
- [ ] Namnformat för konfliktkopior
- [ ] SSE-kanal som säger till att pulla
- [ ] Tombstones för borttagna filer
EOF

note "Projekt/Nota/CLI-kommandon.md" <<'EOF'
---
tags: [nota, cli]
status: in progress
created: 2026-11-19T08:30:00+01:00
prio: 1
---
# CLI-kommandon

`nota add "text"` skapar i inkorgen. `nota search` söker i FTS-indexet. `nota ls --folder Fotboll` listar.

## Flaggor

- `--json` på allt, för agenter
- `--tag` sätts i frontmatter

Se också [[Sync-protokoll]] för hur `nota sync` fungerar.
EOF

note "Projekt/Nota/Namnidéer.md" <<'EOF'
---
tags: [nota]
created: 2026-11-21T21:05:00+01:00
---
# Namnidéer

Nota vann. *Notula* var tvåa, ~~Anteckna~~ föll bort direkt.
EOF

note "Projekt/Schackklubb/Bänkar i Göteborg.md" <<'EOF'
---
tags: [schackklubb]
created: 2026-11-02T14:00:00+01:00
---
# Bänkar i Göteborg

Slottsskogen vid Plikta har två bord. Kungsparken saknar tak, funkar inte i regn.

- [x] Lägg in Plikta
- [ ] Fota Kungsparken
EOF

note "Fotboll/Junior träning tisdag.md" <<'EOF'
---
tags: [fotboll, junior]
created: 2026-11-20T18:00:00+01:00
---
# Junior: träning tisdag

19:00 på Valhalla. Ta med västar och koner.

## Upplägg

- Rondo 5 mot 2
- Omställning från hörna
- Spel 8 mot 8
EOF

note "Fotboll/Futsal december.md" <<'EOF'
---
tags: [fotboll, futsal]
created: 2026-11-15T09:00:00+01:00
---
# Futsal: december

Borta mot Örebro 7 dec, hemma 14 dec i Lisebergshallen. #futsal
EOF

note "Hem/Ella 1-årskontroll.md" <<'EOF'
---
tags: [ella]
status: in progress
created: 2026-11-10T07:45:00+01:00
---
# Ella: 1-årskontroll

BVC torsdag 09:30. Frågor: sömn, smakportioner, vaccin.

- [x] Boka tid
- [ ] Skriv ner frågor
EOF

note "Hem/Julklappar 2026.md" <<'EOF'
---
tags: [jul]
created: 2026-11-01T20:10:00+01:00
budget: 3000
klart: false
---
# Julklappar 2026

- [ ] Sara: The Will of the Many
- [x] Ella: stapelleksak
- [ ] Pappa: kaffekvarn
EOF

note "Recept/Pasta alla vodka.md" <<'EOF'
---
tags: [recept, pasta]
created: 2026-11-08T17:30:00+01:00
portioner: 4
---
# Pasta alla vodka

Schalottenlök, tomatpuré stekt hårt, en skvätt vodka, grädde, parmesan. Rigatoni.
EOF

note "Recept/Kimchi-stekt ris.md" <<'EOF'
---
tags: [recept, snabbt]
created: 2026-11-02T12:00:00+01:00
---
# Kimchi-stekt ris

Dagsgammalt ris, kimchi, smör, gochujang. Stekt ägg ovanpå.
EOF

note "Inbox/Dela en anteckning via länk.md" <<'EOF'
---
tags: [idé, nota]
created: 2026-11-22T10:58:00+01:00
---
Dela en anteckning via länk

Läs-länk som går ut efter 7 dagar. Servern har redan blobben, så det är bara en signerad URL.

Kolla hur Bear gör.
EOF

note "Inbox/Ringa Elbolaget om fakturan.md" <<'EOF'
---
tags: [hem]
created: 2026-11-22T09:12:00+01:00
---
Ringa Elbolaget om fakturan

Dubbeldebiterade i oktober, 349 kr. Ärendenummer finns i mejlet.
EOF

note "Inbox/Futsal testa 2-2 i powerplay.md" <<'EOF'
---
tags: [taktik]
created: 2026-11-21T22:15:00+01:00
---
Futsal: testa 2-2 i powerplay

Målvakt ut, fyra ute plus en. Bredd på kanterna, pivot håller boll.
EOF

note "Inbox/Smash burgers med kimchi-mayo.md" <<'EOF'
---
created: 2026-11-21T19:40:00+01:00
---
Smash burgers med kimchi-mayo

Sågs på TikTok. Gochujang + majonnäs + lite lime.
EOF

note ".nota/views/utkast.yaml" <<'EOF'
name: Utkast
icon: pen
where: { status: draft }
sort: updated desc
EOF

note ".nota/views/pagar.yaml" <<'EOF'
name: Pågår
icon: flag
where: { status: in progress }
sort: updated desc
EOF

echo "demo vault in $dir"
