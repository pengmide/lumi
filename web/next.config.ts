import { PHASE_DEVELOPMENT_SERVER } from 'next/constants'
import type { NextConfig } from 'next'

const backendTarget = process.env.NEXT_PUBLIC_API_PROXY_TARGET ?? 'http://127.0.0.1:3000'

export default function nextConfig(phase: string): NextConfig {
  return {
    ...(phase === PHASE_DEVELOPMENT_SERVER
      ? {}
      : {
          output: 'export',
          distDir: 'dist',
          images: {
            unoptimized: true,
          },
        }),
    ...(phase === PHASE_DEVELOPMENT_SERVER
      ? {
          async rewrites() {
            return [
              {
                source: '/api/:path*',
                destination: `${backendTarget}/api/:path*`,
              },
            ]
          },
        }
      : {}),
  }
}
