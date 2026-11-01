// The components the docs' MDX gets beyond HTML (pages/[...slug].astro). `:::note` and `:::caution` become callouts
// (remarkDirectiveAdmonition, astro.config.mjs) with the title Starlight's asides had.
import type { ReactNode } from 'react';
import { CalloutContainer as Container, CalloutDescription, CalloutTitle } from 'fumadocs-ui/components/callout';

const titles: Record<string, string> = { info: 'Note', warning: 'Caution', error: 'Danger', success: 'Tip' };

function CalloutContainer({ type = 'info', children }: { type?: 'info' | 'warning' | 'error' | 'success'; children?: ReactNode }) {
	return (
		<Container type={type} className="aside" data-aside={type}>
			<CalloutTitle>{titles[type]}</CalloutTitle>
			{children}
		</Container>
	);
}

export const mdxComponents = { CalloutContainer, CalloutDescription, CalloutTitle };
