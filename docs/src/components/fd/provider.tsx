// The root of every React island: Fumadocs' context, with full-page navigation. Without `navigate`, the router does
// `location.assign`, and links are plain <a>, so each page loads afresh and the scenes' scripts run on it, as before.
import type { ReactNode } from 'react';
import { RootProvider } from 'fumadocs-ui/provider/astro';
import SearchDialog from './search';

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
			// tokens.css switches on :root[data-theme]; Fumadocs' own styles on .dark.
			theme={{ attribute: ['class', 'data-theme'], defaultTheme: 'system', enableSystem: true }}
		>
			{children}
		</RootProvider>
	);
}
