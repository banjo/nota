import { Toaster as Sonner, type ToasterProps } from "sonner";

export { toast } from "sonner";

/** The app's toasts: quiet surface, follows the system theme. */
export function Toaster(props: ToasterProps) {
  return (
    <Sonner
      theme="system"
      position="bottom-right"
      gap={8}
      toastOptions={{
        classNames: {
          toast:
            "!rounded-lg !bg-surface !text-foreground !shadow-pop !border-0 !text-ui !gap-2 !font-sans",
          description: "!text-muted-foreground",
          actionButton:
            "!bg-ink !text-ink-foreground !rounded-md !h-7 !px-2.5 !text-small !font-medium",
          cancelButton:
            "!bg-chip !text-foreground !rounded-md !h-7 !px-2.5 !text-small !font-medium",
        },
      }}
      {...props}
    />
  );
}
