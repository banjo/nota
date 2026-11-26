// Reads every note in Apple Notes as JSON (importer.AppleNote): its
// folder path, title, HTML body and dates. Run by osascript -l JavaScript.
// eslint-disable-next-line no-unused-vars -- osascript calls it
function run() {
  const Notes = Application("Notes");
  const accounts = Notes.accounts();
  const out = [];
  const seen = {};
  // The trash ("Recently Deleted") is a folder like the others to
  // scripting, by its name in Notes' language: Notes' own translations.
  const trash = { "Recently Deleted": true };
  try {
    ObjC.import("Foundation");
    for (const p of [
      "/System/Library/PrivateFrameworks/NotesShared.framework/Resources/Localizable.loctable",
      "/System/Applications/Notes.app/Contents/Resources/Localizable.loctable",
    ]) {
      const d = ObjC.deepUnwrap($.NSDictionary.dictionaryWithContentsOfFile(p));
      for (const lang in d || {}) {
        const name = d[lang] && d[lang]["Recently Deleted"];
        if (name) trash[name] = true;
      }
    }
  } catch {}
  const walk = (folder, prefix) => {
    const id = folder.id();
    if (seen[id]) return;
    seen[id] = true;
    const path = prefix ? prefix + "/" + folder.name() : folder.name();
    const notes = folder.notes;
    const ids = notes.id();
    if (ids.length) {
      const names = notes.name();
      const created = notes.creationDate();
      const modified = notes.modificationDate();
      const locked = notes.passwordProtected();
      let bodies = null;
      try {
        bodies = notes.body();
      } catch {}
      for (let i = 0; i < ids.length; i++) {
        let body = "";
        let attachments = 0;
        if (!locked[i]) {
          try {
            body = bodies ? bodies[i] : notes[i].body();
          } catch {}
          // Files have names; tables and tags, which are in the body
          // already, have none.
          try {
            attachments = notes[i].attachments.name().filter((n) => n).length;
          } catch {}
        }
        out.push({
          id: ids[i],
          name: names[i],
          folder: path,
          body: body,
          created: created[i].toISOString(),
          modified: modified[i].toISOString(),
          locked: locked[i],
          attachments: attachments,
        });
      }
    }
    for (const sub of folder.folders()) walk(sub, path);
  };
  for (const acc of accounts) {
    const prefix = accounts.length > 1 ? acc.name() : "";
    // A folder's container can't be read: the top folders are those no
    // other folder has inside it.
    const folders = acc.folders();
    const inner = {};
    for (const f of folders) for (const id of f.folders.id()) inner[id] = true;
    for (const f of folders) {
      if (inner[f.id()] || trash[f.name()]) continue;
      walk(f, prefix);
    }
  }
  return JSON.stringify(out);
}
