import { FilePlus2, FolderPlus } from "lucide-react";
import { useRunCommand } from "../lib/runCommand.ts";
import { IconButton } from "./NewFolder.tsx";

/**
 * "New note" and "New folder" at the right end of the Folders header.
 * They run the ⌘K commands (note.new, folder.add), so the palette, the
 * shortcut and these buttons always do the same. Always visible: a phone
 * has no hover.
 */
export function FoldersHeaderActions() {
  const run = useRunCommand();
  return (
    <div className="absolute right-0.5 bottom-0 flex items-center gap-px pointer-coarse:bottom-0.5 pointer-coarse:right-0">
      <IconButton
        label="New note"
        onClick={() => run("note.new")}
        className="size-6 pointer-coarse:size-10"
      >
        <FilePlus2 strokeWidth={1.75} />
      </IconButton>
      <IconButton
        label="New folder"
        onClick={() => run("folder.add")}
        className="size-6 pointer-coarse:size-10"
      >
        <FolderPlus strokeWidth={1.75} />
      </IconButton>
    </div>
  );
}
