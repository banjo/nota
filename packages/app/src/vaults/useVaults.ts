import type { VaultSummary } from "@nota/client";
import { toast } from "@nota/ui/sonner";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { fail } from "../actions.ts";
import { useClient } from "../context.ts";
import { afterDelete, onlineVaults, switchVault, vaultsKey } from "./online.ts";

/** The account's server vaults and what can be done with them (web only). */
export function useVaults() {
  const client = useClient();
  const qc = useQueryClient();
  const online = onlineVaults(client);
  const list = useQuery({
    queryKey: vaultsKey,
    queryFn: () => online!.auth.vaults(),
    enabled: !!online,
  });
  const refresh = () => qc.invalidateQueries({ queryKey: vaultsKey });
  const current = online?.current.name ?? "";

  const create = useMutation({
    mutationFn: ({ name }: { name: string; open?: boolean }) => online!.auth.createVault(name),
    onSuccess: (v, { open }) => {
      if (open) return switchVault(client, v.name);
      toast.success(`Created ${v.name}`, {
        action: { label: "Open", onClick: () => switchVault(client, v.name) },
      });
      void refresh();
    },
    onError: (e) => fail("Couldn't create the vault", e),
  });

  const rename = useMutation({
    mutationFn: ({ name, to }: { name: string; to: string }) => online!.auth.renameVault(name, to),
    onSuccess: (v, { name }) => {
      // The open vault's URLs carry its name: reload under the new one.
      if (name === current) return switchVault(client, v.name);
      toast.success(`Renamed ${name} to ${v.name}`);
      qc.setQueryData<VaultSummary[]>(vaultsKey, (vs) =>
        vs?.map((x) => (x.name === name ? v : x)).sort((a, b) => a.name.localeCompare(b.name)),
      );
      void refresh();
    },
    onError: (e) => fail("Couldn't rename the vault", e),
  });

  const remove = useMutation({
    mutationFn: ({ name, confirm }: { name: string; confirm: string }) =>
      online!.auth.deleteVault(name, confirm),
    onSuccess: (_, { name }) => {
      const next = afterDelete(current, name, list.data ?? []);
      if (next !== undefined) return switchVault(client, next);
      toast.success(`Deleted ${name}`);
      qc.setQueryData<VaultSummary[]>(vaultsKey, (vs) => vs?.filter((x) => x.name !== name));
      void refresh();
    },
    onError: (e) => fail("Couldn't delete the vault", e),
  });

  return { online, current, list, create, rename, remove };
}
