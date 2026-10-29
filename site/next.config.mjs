import { createMDX } from 'fumadocs-mdx/next';

const withMDX = createMDX();

/** @type {import('next').NextConfig} */
const config = {
  output: 'export',
  // GitHub Pages serves `/start/` from `start/index.html`, as the Starlight site's URLs do now.
  trailingSlash: true,
  // A static export has no image server.
  images: { unoptimized: true },
  reactStrictMode: true,
};

export default withMDX(config);
