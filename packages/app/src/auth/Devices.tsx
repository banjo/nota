import type { Device } from "@nota/client";
import { Button } from "@nota/ui/button";
import { Laptop } from "lucide-react";
import { relativeTime } from "../lib/format.ts";

/** The devices of an account: shared by Settings and the web app's page. */
export function DeviceList({
  devices,
  onRevoke,
  busy,
}: {
  devices: Device[];
  onRevoke: (device: Device) => void;
  busy?: boolean;
}) {
  return (
    <ul className="overflow-hidden rounded-lg border border-hairline">
      {devices.map((d) => (
        <li
          key={d.id}
          className="flex items-center gap-3 border-t border-divider px-3.5 py-2.5 text-ui first:border-t-0 pointer-coarse:py-3"
        >
          <Laptop className="size-4 shrink-0 text-faint" strokeWidth={1.75} />
          <div className="min-w-0 flex-1">
            <div className="truncate text-foreground">
              {d.device}
              {d.current ? <span className="ml-2 text-small text-ink">This device</span> : null}
            </div>
            <div className="text-small text-faint">
              {d.lastUsedAt
                ? `Last seen ${relativeTime(d.lastUsedAt).toLowerCase()}`
                : `Added ${relativeTime(d.createdAt).toLowerCase()}`}
            </div>
          </div>
          {!d.current ? (
            <Button variant="ghost" size="sm" onClick={() => onRevoke(d)} disabled={busy}>
              Sign out
            </Button>
          ) : null}
        </li>
      ))}
    </ul>
  );
}
