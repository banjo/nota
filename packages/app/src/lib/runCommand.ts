import { useQueryClient } from "@tanstack/react-query";
import { type CommandContext, useRegistry } from "../registry.ts";
import { useClient } from "../context.ts";
import { currentPath, useUI } from "../state.ts";

/** Runs a registered ⌘K command by id, from a button instead of the palette. */
export function useRunCommand() {
  const registry = useRegistry();
  const client = useClient();
  const qc = useQueryClient();
  return (id: string) => {
    const cmd = registry.command(id);
    if (!cmd) return;
    const ui = useUI.getState();
    const ctx: CommandContext = {
      client,
      queryClient: qc,
      route: ui.route,
      notePath: currentPath(ui.route),
      navigate: ui.navigate,
    };
    if (cmd.when && !cmd.when(ctx)) return;
    void cmd.run(ctx);
  };
}
