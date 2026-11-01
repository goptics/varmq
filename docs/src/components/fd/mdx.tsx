// The components the docs' MDX gets beyond HTML (pages/[...slug].astro). `:::note` and `:::caution` become
// CalloutContainer (remarkDirectiveAdmonition, astro.config.mjs), drawn as the docs' asides (fd-theme.css, "Callout").
// Rendered on the server only: none of them needs the browser.
import type { ComponentProps, ReactNode } from 'react';

const kinds: Record<string, { name: string; title: string }> = {
	info: { name: 'note', title: 'Note' },
	success: { name: 'tip', title: 'Tip' },
	warning: { name: 'caution', title: 'Caution' },
	error: { name: 'danger', title: 'Danger' },
};

function CalloutContainer({ type = 'info', children }: { type?: string; children?: ReactNode }) {
	const kind = kinds[type] ?? kinds.info;
	return (
		<aside aria-label={kind.title} className={`doc-aside doc-aside--${kind.name}`}>
			<p className="doc-aside__title" aria-hidden="true">
				{kind.title}
			</p>
			{children}
		</aside>
	);
}

function CalloutTitle({ children }: { children?: ReactNode }) {
	return <p className="doc-aside__title">{children}</p>;
}

function CalloutDescription({ children }: { children?: ReactNode }) {
	return <div className="doc-aside__content">{children}</div>;
}

function table(props: ComponentProps<'table'>) {
	return (
		<div className="doc-table">
			<table {...props} />
		</div>
	);
}

export const mdxComponents = { CalloutContainer, CalloutTitle, CalloutDescription, table };
