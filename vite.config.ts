import react from "@vitejs/plugin-react";
import { defineConfig } from "vite";

export default defineConfig({
  root: "web",
  plugins: [react()],
  resolve: {
    alias: [
      { find: /^monaco-editor$/, replacement: "monaco-editor/esm/vs/editor/editor.api" },
    ],
  },
  build: {
    outDir: "dist",
    emptyOutDir: true,
    chunkSizeWarningLimit: 4500,
    rollupOptions: {
      output: {
        manualChunks(id) {
          const has = (needle: string) => id.indexOf(needle) >= 0;
          if (!has("node_modules")) return;
          if (has("monaco-editor") || has("@monaco-editor") || has("@nginx/reference-lib")) return "vendor-monaco";
          if (has("@xterm")) return "vendor-terminal";
          if (has("lucide-react")) return "vendor-icons";
          if (has("react") || has("@tanstack/react-query")) return "vendor-react";
        },
      },
    },
  },
  server: {
    port: 5173,
    strictPort: true,
    proxy: {
      "/api": "http://127.0.0.1:18080",
      "/install": "http://127.0.0.1:18080",
    },
  },
});
