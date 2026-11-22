import type { AuthApi, Device } from "@nota/client";
import { Button } from "@nota/ui/button";
import { toast } from "@nota/ui/sonner";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { ArrowLeft, KeyRound, LoaderCircle, ShieldCheck } from "lucide-react";
import { type ReactNode, useState } from "react";
import { fail } from "../actions.ts";
import { relativeTime } from "../lib/format.ts";
import { DeviceList } from "./Devices.tsx";
import { useSession } from "./session.ts";

function Section({
  title,
  description,
  children,
}: {
  title: string;
  description?: string;
  children: ReactNode;
}) {
  return (
    <section className="border-t border-divider py-6 first:border-t-0 first:pt-0">
      <h2 className="text-[14px] font-semibold text-foreground">{title}</h2>
      {description ? <p className="mt-1 text-ui text-muted-foreground">{description}</p> : null}
      <div className="mt-4">{children}</div>
    </section>
  );
}

/**
 * /app/devices: the account's signed-in devices and passkeys. A token
 * signed in from the CLI or the desktop can be revoked here; revoking
 * one only stops that device from syncing.
 */
export function DevicesPage({ auth, onBack }: { auth: AuthApi; onBack: () => void }) {
  const session = useSession();
  const qc = useQueryClient();
  const [revoking, setRevoking] = useState(false);

  const devices = useQuery({ queryKey: ["devices"], queryFn: () => auth.devices() });
  const passkeys = useQuery({ queryKey: ["passkeys"], queryFn: () => auth.passkeys() });

  const revoke = useMutation({
    mutationFn: (d: Device) => auth.revokeDevice(d.id),
    onMutate: () => setRevoking(true),
    onSuccess: (_, d) => {
      void qc.invalidateQueries({ queryKey: ["devices"] });
      toast.success(`Signed out ${d.device}`);
    },
    onError: (e) => fail("Couldn't sign the device out", e),
    onSettled: () => setRevoking(false),
  });

  const removePasskey = useMutation({
    mutationFn: (id: string) => auth.removePasskey(id),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: ["passkeys"] });
    },
    onError: (e) => fail("Couldn't remove the passkey", e),
  });

  return (
    <div className="h-full overflow-y-auto bg-background">
      <div className="mx-auto w-full max-w-[640px] px-14 pt-8 pb-24 max-[720px]:px-5 max-[720px]:pt-5">
        <button
          type="button"
          onClick={onBack}
          className="-ml-1 mb-6 flex items-center gap-1.5 rounded-sm px-1 py-0.5 text-ui text-muted-foreground outline-none hover:text-foreground focus-visible:ring-2 focus-visible:ring-ink"
        >
          <ArrowLeft className="size-4" /> Back to notes
        </button>
        <h1 className="mb-1 text-[26px] font-bold tracking-[-0.015em] text-foreground">
          Devices and security
        </h1>
        <p className="mb-8 text-ui text-muted-foreground">
          {session?.user.email ?? ""} · signed in on this browser
        </p>

        <Section
          title="Devices"
          description="Each signed-in app or CLI has its own token. Revoking one stops it from syncing; the notes it already has stay where they are."
        >
          {devices.isError ? (
            <p className="text-ui text-muted-foreground">
              Couldn't load devices: {String(devices.error)}
            </p>
          ) : devices.isPending ? (
            <LoaderCircle className="size-4 animate-spin text-faint motion-reduce:animate-none" />
          ) : devices.data.length ? (
            <DeviceList devices={devices.data} onRevoke={(d) => revoke.mutate(d)} busy={revoking} />
          ) : (
            <p className="text-ui text-muted-foreground">No device tokens yet.</p>
          )}
          <p className="mt-3 flex items-center gap-1.5 text-small text-faint">
            <ShieldCheck className="size-3.5" /> This browser is a session, not a device token; sign
            out from Settings to end it.
          </p>
        </Section>

        <Section
          title="Passkeys"
          description="Sign in with Touch ID, Windows Hello or a security key. A passkey never leaves your device."
        >
          {passkeys.isPending ? (
            <LoaderCircle className="size-4 animate-spin text-faint motion-reduce:animate-none" />
          ) : (
            <ul className="overflow-hidden rounded-lg border border-hairline">
              {(passkeys.data ?? []).map((p) => (
                <li
                  key={p.id}
                  className="flex items-center gap-3 border-t border-divider px-3.5 py-2.5 text-ui first:border-t-0"
                >
                  <KeyRound className="size-4 shrink-0 text-faint" strokeWidth={1.75} />
                  <div className="min-w-0 flex-1">
                    <div className="truncate text-foreground">{p.name}</div>
                    <div className="text-small text-faint">
                      {p.lastUsedAt
                        ? `Used ${relativeTime(p.lastUsedAt).toLowerCase()}`
                        : "Not used yet"}
                    </div>
                  </div>
                  {(passkeys.data?.length ?? 0) > 1 ? (
                    <Button
                      variant="ghost"
                      size="sm"
                      disabled={removePasskey.isPending}
                      onClick={() => removePasskey.mutate(p.id)}
                    >
                      Remove
                    </Button>
                  ) : null}
                </li>
              ))}
            </ul>
          )}
          <p className="mt-3 text-small text-faint">
            {session ? `${session.recoveryCodesLeft} recovery codes left.` : ""}
          </p>
        </Section>

        <Section
          title="Session"
          description="Signing out ends this browser session. Your passkey stays."
        >
          <Button
            variant="outline"
            onClick={() =>
              void auth
                .logout()
                .then(() => onBack())
                .catch((e: unknown) => fail("Couldn't sign out", e))
            }
          >
            Sign out of the web app
          </Button>
        </Section>
      </div>
    </div>
  );
}
