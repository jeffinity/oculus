import react from "@vitejs/plugin-react";
import { defineConfig } from "vite";

export default defineConfig({
  plugins: [react()],
  build: {
    chunkSizeWarningLimit: 1200,
    rollupOptions: {
      output: {
        manualChunks(id) {
          if (!id.includes("node_modules")) {
            return null;
          }
          if (id.includes("node_modules/react-dom") || id.includes("node_modules/react")) {
            return "react-vendor";
          }
          if (id.includes("node_modules/antd") || id.includes("node_modules/@ant-design/icons")) {
            return "antd-vendor";
          }
          return null;
        }
      }
    }
  },
  server: {
    host: true,
    port: 5175,
    proxy: {
      "/api": {
        target: "http://127.0.0.1:7301",
        changeOrigin: true
      }
    }
  }
});
