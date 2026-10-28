// SPDX-License-Identifier: Apache-2.0
import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import App from "./App";
import "./styles/tokens.css";
import "./styles/global.css";
import "./styles/g02.css";
import "./styles/g03.css";
import "./styles/g05.css";
import "./styles/living-world.css";
import "./styles/v2.css";
import "./styles/phase2.css";
import "./styles/phase2c.css";

const rootElement = document.getElementById("root");
if (!rootElement) {
  throw new Error("root element is missing");
}

createRoot(rootElement).render(
  <StrictMode>
    <App />
  </StrictMode>
);
