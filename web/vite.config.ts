import { defineConfig } from "vite";
import vue from "@vitejs/plugin-vue";
import wasm from "vite-plugin-wasm";

export default defineConfig({
  plugins: [wasm(), vue()],
  server: {
    host: "127.0.0.1",
    port: 5173,
    proxy: {
      "/api": "http://127.0.0.1:8080"
    }
  },
  build: {
    target: "es2022",
    sourcemap: false,
    emptyOutDir: true
  }
});
