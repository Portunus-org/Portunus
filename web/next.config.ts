import type { NextConfig } from "next"

const isDev = process.env.NODE_ENV === "development"

// During `next dev`, API calls are proxied to a locally running panel.
const apiOrigin = process.env.PORTUNUS_API ?? "http://127.0.0.1:2053"

const nextConfig: NextConfig = {
  // Production build is a static export embedded into the Go binary; there
  // is no Node.js runtime in the container.
  output: isDev ? undefined : "export",
  trailingSlash: true,
  images: { unoptimized: true },
  poweredByHeader: false,
  productionBrowserSourceMaps: false,
  ...(isDev && {
    // Keep /api/foo from being redirected to /api/foo/ before the rewrite.
    skipTrailingSlashRedirect: true,
    async rewrites() {
      return [
        { source: "/api/:path*", destination: `${apiOrigin}/api/:path*` },
        { source: "/healthz", destination: `${apiOrigin}/healthz` },
      ]
    },
  }),
}

export default nextConfig
