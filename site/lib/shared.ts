import { createGetUrl } from 'fumadocs-core/source';

export const appName = 'VarMQ';
export const installCommand = 'go get github.com/goptics/varmq';
// The published quick start, until the docs move into this site; the migration decides its path here.
export const quickStartUrl = 'https://varmq.goptics.org/start/quick-start/';
export const docsRoute = '/docs';
export const docsImageRoute = '/og/docs';
export const docsContentRoute = '/llms.mdx/docs';

export const gitConfig = {
  user: 'goptics',
  repo: 'varmq',
  branch: 'main',
  // where this app sits in the repository
  dir: 'site',
};

const getContentUrl = createGetUrl(docsContentRoute);

export function getPageMarkdownUrl(page: { slugs: string[]; locale?: string }) {
  const segments = [...page.slugs, 'content.md'];

  return { segments, url: getContentUrl(segments, page.locale) };
}

const getImageUrl = createGetUrl(docsImageRoute);

export function getPageImageUrl(page: { slugs: string[]; locale?: string }) {
  const segments = [...page.slugs, 'image.png'];

  return { segments, url: getImageUrl(segments, page.locale) };
}
