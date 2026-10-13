import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import "./index.css";
import App from "./App";

async function prepare() {
  if (import.meta.env.VITE_DEMO_MODE === "true") {
    const { worker } = await import("./mocks/browser");
    return worker.start({
      onUnhandledRequest: "bypass",
      serviceWorker: { url: `${import.meta.env.BASE_URL.replace(/\/$/, '')}/mockServiceWorker.js` },
    });
  }
}

prepare().then(() => {
  createRoot(document.getElementById("root")).render(
    <StrictMode>
      <App />
    </StrictMode>
  );
});
