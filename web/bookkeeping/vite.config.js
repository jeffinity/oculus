import react from "@vitejs/plugin-react";
import { visualizer } from "rollup-plugin-visualizer";
import { defineConfig } from "vite";

export default defineConfig(({ command }) => ({
  plugins: [
    react(),
    command === "build"
      ? visualizer({
          filename: "dist/stats.html",
          template: "treemap",
          gzipSize: true,
          brotliSize: true,
          open: false
        })
      : null
  ].filter(Boolean),
  server: {
    host: true,
    port: 5173
  },
  build: {
    sourcemap: false,
    chunkSizeWarningLimit: 2000,
    rollupOptions: {
      output: {
        manualChunks(id) {
          if (!id.includes("node_modules")) {
            return null;
          }
          if (id.includes("@ant-design/charts") || id.includes("@antv")) {
            return "charts-vendor";
          }
          if (id.includes("/dayjs/")) {
            return "dayjs-vendor";
          }
          if (id.includes("/react-dom/") || id.includes("/react/") || id.includes("/scheduler/")) {
            return "react-vendor";
          }
          return null;
        }
      }
    }
  }
}));
