// The three places jobs live, as their tabs show them: the name and the mark (a memory chip, then the stores' logos
// the hero map draws). JOB 02 on the home page and SetupSteps' storage tabs share them, so the tab bars match.
import { LOGOS } from '../../lib/heromap/render';

export const STORES = {
	memory: {
		label: 'In memory',
		// A memory chip, drawn on the logos' 24-unit grid.
		icon: 'M5 5h14v14H5zm3 3v8h8V8zM8 1h2v3H8zm3 0h2v3h-2zm3 0h2v3h-2zM8 20h2v3H8zm3 0h2v3h-2zm3 0h2v3h-2zM1 8h3v2H1zm0 3h3v2H1zm0 3h3v2H1zm19-6h3v2h-3zm0 3h3v2h-3zm0 3h3v2h-3z',
	},
	sqlite: { label: 'SQLite', icon: LOGOS['s-store'].d },
	redis: { label: 'Redis', icon: LOGOS['r-store'].d },
} as const;

export type StoreKey = keyof typeof STORES;
