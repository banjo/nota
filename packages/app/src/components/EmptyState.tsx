import type { LucideIcon } from "lucide-react";
import type { ReactNode } from "react";

/** An empty screen says what to do next. */
export function EmptyState({
  icon: Icon,
  title,
  body,
  action,
}: {
  icon: LucideIcon;
  title: string;
  body?: ReactNode;
  action?: ReactNode;
}) {
  return (
    <div className="mx-auto flex max-w-sm flex-1 flex-col items-center justify-center gap-2 px-6 py-24 text-center">
      <Icon className="mb-2 size-7 text-faint" strokeWidth={1.5} />
      <h2 className="text-[15px] font-semibold text-foreground">{title}</h2>
      {body ? <p className="text-ui text-muted-foreground">{body}</p> : null}
      {action ? <div className="mt-3">{action}</div> : null}
    </div>
  );
}
