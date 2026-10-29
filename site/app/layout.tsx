import type { Metadata } from 'next';
import { Inter } from 'next/font/google';
import { Provider } from '@/components/provider';
import { appName } from '@/lib/shared';
import './global.css';

const inter = Inter({
  subsets: ['latin'],
});

export const metadata: Metadata = {
  // Social images and canonical URLs resolve against the published site, not the build machine.
  metadataBase: new URL('https://varmq.goptics.org'),
  title: { default: appName, template: `%s | ${appName}` },
  description:
    'A Simplest in-memory and Distributed Storage-Agnostic Message Queue for Your Concurrent Go Program',
};

export default function Layout({ children }: LayoutProps<'/'>) {
  return (
    <html lang="en" className={inter.className} suppressHydrationWarning>
      <body className="flex flex-col min-h-screen">
        <Provider>{children}</Provider>
      </body>
    </html>
  );
}
