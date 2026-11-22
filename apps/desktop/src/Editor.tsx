import { createEditor } from "@nota/editor";
import { useEffect, useRef } from "react";

export function Editor({ doc, onSave }: { doc: string; onSave: (body: string) => void }) {
  const ref = useRef<HTMLDivElement>(null);
  const save = useRef(onSave);
  save.current = onSave;

  useEffect(() => {
    if (!ref.current) return;
    const view = createEditor(ref.current, { doc, onChange: (body) => save.current(body) });
    return () => view.destroy();
  }, [doc]);

  return <div ref={ref} className="editor" />;
}
