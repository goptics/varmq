// The root of every React island: Fumadocs' context, with full-page navigation. Without `navigate`, the router does
// `location.assign`, and links are plain <a>, so each page loads afresh and the scenes' scripts run on it, as before.
import { lazy, type ReactNode } from 'react';
import { RootProvider } from 'fumadocs-ui/provider/astro';

// Its own chunk, so the search UI and index client are not in every page's first bundle. Fumadocs mounts it in a
// Suspense after hydration, so the dialog is ready before a reader opens it. A chunk that fails to load costs search
// alone: a rejected lazy() would stop the whole island hydrating, and the sidebar and theme switch with it.
const SearchDialog = lazy(() => import('./search').catch(() => ({ default: () => null })));

export interface ProviderProps {
	pathname: string;
	params?: Record<string, string | undefined>;
	children?: ReactNode;
}

export function Provider({ pathname, params, children }: ProviderProps) {
	return (
		<RootProvider
			pathname={pathname}
			params={params}
			search={{ SearchDialog }}
			// tokens.css switches on :root[data-theme]; Fumadocs' own styles on .dark. No `d` shortcut: the site never had one.
			theme={{ attribute: ['class', 'data-theme'], defaultTheme: 'system', enableSystem: true, hotKey: false }}
		>
			{children}
		</RootProvider>
	);
}
