import { NotaApp } from "@nota/app";
import { createMygoClient } from "@nota/client";
import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { Account, Desktop, events, Vault } from "./mygo.ts";
import "./style.css";

const client = createMygoClient({ Vault, Account, Desktop, events });

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <NotaApp client={client} />
  </StrictMode>,
);
