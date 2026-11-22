import { createRoot } from "react-dom/client";
import { StrictMode } from "react";
import { Root } from "./Root.tsx";
import "./style.css";

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <Root />
  </StrictMode>,
);
