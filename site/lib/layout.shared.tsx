import type { BaseLayoutProps } from 'fumadocs-ui/layouts/shared';
import { gitConfig } from './shared';

export function baseOptions(): BaseLayoutProps {
  return {
    nav: {
      // The VarMQ logo, its dark-ink version on light pages; the home page always shows the light-ink one.
      title: (
        <>
          <img src="/logo-light.svg" alt="VarMQ" width={100} height={26} className="hp-logo-light dark:hidden" />
          <img src="/logo-dark.svg" alt="VarMQ" width={100} height={26} className="hp-logo-dark hidden dark:block" />
        </>
      ),
    },
    githubUrl: `https://github.com/${gitConfig.user}/${gitConfig.repo}`,
  };
}
