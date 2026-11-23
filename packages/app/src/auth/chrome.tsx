import { Lockup } from "@nota/ui/logo";
import type { ReactNode } from "react";

/**
 * One calm column for everything that happens before the app: the same
 * warm background and hairlines as the vault, one card, no decoration.
 */
export function AuthShell({
  title,
  description,
  children,
  footer,
  width = 380,
}: {
  title: ReactNode;
  description?: ReactNode;
  children: ReactNode;
  footer?: ReactNode;
  width?: number;
}) {
  return (
    <div className="flex min-h-full flex-col items-center justify-center bg-background px-5 py-12">
      <div className="flex w-full flex-col items-center" style={{ maxWidth: width }}>
        <a
          href="/app/"
          className="mb-7 flex items-center gap-2 text-foreground outline-none focus-visible:underline"
        >
          <Lockup />
        </a>
        <div className="w-full rounded-xl bg-surface p-7 shadow-card max-[420px]:p-5">
          <h1 className="text-[20px] font-semibold tracking-[-0.015em] text-foreground">{title}</h1>
          {description ? (
            <p className="mt-1.5 text-ui leading-[1.5] text-muted-foreground">{description}</p>
          ) : null}
          <div className="mt-5">{children}</div>
        </div>
        {footer ? (
          <div className="mt-5 w-full text-center text-ui text-muted-foreground">{footer}</div>
        ) : null}
        <p className="mt-8 text-small text-faint">
          Local-first notes in plain Markdown ·{" "}
          <a className="underline-offset-2 hover:text-muted-foreground hover:underline" href="/">
            About Nota
          </a>
        </p>
      </div>
    </div>
  );
}
