/** @type {import('next').NextConfig} */
const nextConfig = {
  output: "standalone",
  async rewrites() {
    const apiUrl = process.env.NEXT_PUBLIC_API_URL;
    // NEXT_PUBLIC_API_URL is only available at runtime (injected by docker-compose).
    // Return an empty array at build time so `next build` doesn't fail.
    if (!apiUrl) return [];
    return [
      {
        source: "/api/v1/:path*",
        destination: `${apiUrl}/api/v1/:path*`,
      },
    ];
  },
};

export default nextConfig;
