import { createMygoClient } from "@nota/client";
import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { App } from "./App.tsx";
import { Account, Desktop, events, Vault } from "./mygo.ts";
import "./style.css";

const client = createMygoClient({ Vault, Account, Desktop, events });

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <App client={client} />
  </StrictMode>,
);
