import { CaptureApp, NotaApp, NotaProviders } from "@nota/app";
import { createMygoClient } from "@nota/client";
import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { Account, Desktop, events, Vault } from "./mygo.ts";
import "./style.css";

const client = createMygoClient({ Vault, Account, Desktop, events });

// The quick-capture panel is a second window of the same app (/?window=capture).
const capture = new URLSearchParams(location.search).get("window") === "capture";

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    {capture ? (
      <NotaProviders client={client}>
        <CaptureApp />
      </NotaProviders>
    ) : (
      <NotaApp client={client} />
    )}
  </StrictMode>,
);
