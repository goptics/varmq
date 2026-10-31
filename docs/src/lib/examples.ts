// The example programs (examples/*/main.go) and the versions they run with (examples/go.mod), read at build time for
// GoPlay.astro on the docs pages and home/Examples.astro on the home page. Server only: the page gets the source in
// its markup, and the browser code (go-play.ts, playground.ts) never imports this module.

import goMod from '../../examples/go.mod?raw';
import { pins } from './playground';

const sources = import.meta.glob<string>('../../examples/*/main.go', {
	query: '?raw',
	import: 'default',
	eager: true,
});

/** The source of examples/<name>/main.go; a name with no such file fails the build. */
export function exampleSource(name: string, from = 'example'): string {
	const src = sources[`../../examples/${name}/main.go`];
	if (src === undefined) throw new Error(`${from}: unknown example "${name}"`);
	return src;
}

/** The go and varmq versions, as `go-play`'s `data-pins` takes them. */
export const versions = JSON.stringify(pins(goMod));
