// The perf harness entry: the real packages/app NotaApp over the in-memory
// client. ?vault=small|large picks the vault, ?big=1 adds the 5000-line note.
import { NotaApp } from "@nota/app";
import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { createMemoryClient } from "./memory-client.ts";
import { bigNote, largeVault, smallVault, VIEWS } from "./seed.ts";
import "./style.css";

const q = new URLSearchParams(location.search);
const seed = q.get("vault") === "large" ? largeVault() : smallVault();
if (q.get("big") === "1") seed.push(bigNote());
const client = createMemoryClient(seed, { views: VIEWS });
(window as unknown as { __notaClient: unknown }).__notaClient = client;

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <NotaApp client={client} />
  </StrictMode>,
);
