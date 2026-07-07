import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";

export default defineConfig({
  base: process.env.VITE_BASE ?? "/",
  publicDir: "public",
  plugins: [react(), tailwindcss()],
  server: {
    proxy: {
      "/api": process.env.API_PROXY || "http://127.0.0.1:8080"
    }
  }
});
