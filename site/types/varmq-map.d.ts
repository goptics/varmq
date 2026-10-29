import 'react';

declare module 'react' {
	namespace JSX {
		interface IntrinsicElements {
			// The map's host, which lib/heromap/map.css styles by its tag; a plain element, never registered.
			'varmq-map': React.DetailedHTMLProps<React.HTMLAttributes<HTMLElement>, HTMLElement>;
		}
	}
}
