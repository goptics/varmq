import type { BaseLayoutProps } from 'fumadocs-ui/layouts/shared';

/** The logo's two files, one per theme (assets/logo-*.svg). */
export interface Logo {
	light: string;
	dark: string;
}

export function baseOptions(logo: Logo): BaseLayoutProps {
	return {
		nav: {
			url: '/',
			title: (
				<>
					<img className="shell-logo shell-logo--light" src={logo.light} alt="VarMQ" width={106} height={30} />
					<img className="shell-logo shell-logo--dark" src={logo.dark} alt="VarMQ" width={106} height={30} />
				</>
			),
		},
		githubUrl: 'https://github.com/goptics/varmq',
		themeSwitch: { mode: 'light-dark-system' },
	};
}
