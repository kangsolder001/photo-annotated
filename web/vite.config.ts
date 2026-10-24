import { createHash } from "node:crypto";
import { readFileSync } from "node:fs";
import { defineConfig, loadEnv, type Plugin } from "vite";
import react from "@vitejs/plugin-react";

// Hash CSP untuk skrip inline di index.html (skrip tema anti-kedip). Nilai yang sama ditulis di
// nginx/security-headers.conf; test src/lib/theme.test.ts memastikan keduanya tidak berbeda.
export function inlineScriptHashes(html: string): string[] {
  return [...html.matchAll(/<script>([\s\S]*?)<\/script>/g)].map(
    (m) => `'sha256-${createHash("sha256").update(m[1]!).digest("base64")}'`,
  );
}

// Membuat dist/_headers (Cloudflare Pages) berisi CSP ketat yang mengizinkan hanya origin API dan
// Turnstile. Origin API diambil dari VITE_API_BASE_URL saat build.
function cloudflareHeaders(apiBase: string): Plugin {
  const apiOrigin = apiBase ? new URL(apiBase).origin : "";
  const hashes = inlineScriptHashes(readFileSync(new URL("./index.html", import.meta.url), "utf8")).join(" ");
  const csp = [
    "default-src 'self'",
    `script-src 'self' ${hashes} https://challenges.cloudflare.com https://static.cloudflareinsights.com`,
    "frame-src https://challenges.cloudflare.com",
    `connect-src 'self' https://cloudflareinsights.com ${apiOrigin}`.trim(),
    `img-src 'self' blob: data: ${apiOrigin}`.trim(),
    "style-src 'self' 'unsafe-inline'",
    "font-src 'self'",
    "object-src 'none'",
    "base-uri 'none'",
    "form-action 'self'",
    "frame-ancestors 'none'",
  ].join("; ");
  return {
    name: "cloudflare-pages-headers",
    apply: "build",
    generateBundle() {
      this.emitFile({
        type: "asset",
        fileName: "_headers",
        source: [
          "/*",
          `  Content-Security-Policy: ${csp}`,
          "  X-Content-Type-Options: nosniff",
          "  Referrer-Policy: no-referrer",
          "  Permissions-Policy: camera=(self), geolocation=(), microphone=()",
          "/assets/*",
          "  Cache-Control: public, max-age=31536000, immutable",
          "",
        ].join("\n"),
      });
      // SPA: semua rute dilayani index.html
      this.emitFile({ type: "asset", fileName: "_redirects", source: "/* /index.html 200\n" });
    },
  };
}

export default defineConfig(({ mode }) => {
  const env = loadEnv(mode, process.cwd(), "");
  return {
    plugins: [react(), cloudflareHeaders(env.VITE_API_BASE_URL ?? "")],
    server: {
      // dev: /api diteruskan ke API (mis. SSH tunnel ke Pi), jadi tidak perlu CORS saat mengembangkan
      proxy: env.VITE_DEV_API_TARGET ? { "/api": env.VITE_DEV_API_TARGET } : undefined,
    },
    test: {
      environment: "jsdom",
      globals: true,
      setupFiles: ["src/test/setup.ts"],
    },
  };
});
