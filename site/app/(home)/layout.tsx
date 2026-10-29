import { HomeLayout } from 'fumadocs-ui/layouts/home';
import { Barlow, Barlow_Condensed } from 'next/font/google';
import localFont from 'next/font/local';
import { baseOptions } from '@/lib/layout.shared';
import './home.css';

// Signage capitals for headings and labels, a plain grotesk for copy, and Go's own mono for code.
const sign = Barlow_Condensed({ subsets: ['latin'], weight: ['500', '600', '700'], variable: '--hp-font-sign' });
const copy = Barlow({ subsets: ['latin'], weight: ['400', '500', '600'], variable: '--hp-font-copy' });
const code = localFont({ src: '../fonts/go-mono.woff', variable: '--hp-font-code', display: 'swap' });

// The home page keeps its enamel in either theme, so it has no theme switch.
export default function Layout({ children }: LayoutProps<'/'>) {
	return (
		<div className={`hp-home ${sign.variable} ${copy.variable} ${code.variable}`}>
			<HomeLayout {...baseOptions()} themeSwitch={{ enabled: false }}>
				{children}
			</HomeLayout>
		</div>
	);
}
