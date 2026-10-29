import { HomeLayout } from 'fumadocs-ui/layouts/home';
import localFont from 'next/font/local';
import { baseOptions } from '@/lib/layout.shared';
import './home.css';

// Go's own typefaces: Go Sans for headings and copy, Go Mono for code and the map's labels.
const sans = localFont({
	src: [
		{ path: '../fonts/go-regular.woff', weight: '400' },
		{ path: '../fonts/go-medium.woff', weight: '500' },
		{ path: '../fonts/go-bold.woff', weight: '700' },
	],
	variable: '--hp-font-sans',
	display: 'swap',
});
const code = localFont({
	src: [
		{ path: '../fonts/go-mono.woff', weight: '400' },
		{ path: '../fonts/go-mono-bold.woff', weight: '700' },
	],
	variable: '--hp-font-code',
	display: 'swap',
});

// The home page keeps its enamel in either theme, so it has no theme switch.
export default function Layout({ children }: LayoutProps<'/'>) {
	return (
		<div className={`hp-home ${sans.variable} ${code.variable}`}>
			<HomeLayout {...baseOptions()} themeSwitch={{ enabled: false }}>{children}</HomeLayout>
		</div>
	);
}
