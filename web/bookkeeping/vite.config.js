import react from "@vitejs/plugin-react";
import { defineConfig } from "vite";

export default defineConfig({
  plugins: [react()],
  server: {
    host: true,
    port: 5173
  },
  build: {
    sourcemap: false,
    rollupOptions: {
      output: {
        manualChunks(id) {
          if (!id.includes("node_modules")) {
            return null;
          }
          if (id.includes("@ant-design/charts") || id.includes("@antv")) {
            return "charts-vendor";
          }
          return null;
        }
      }
    }
  }
});
