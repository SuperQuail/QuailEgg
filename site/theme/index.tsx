import { Layout as BasicLayout } from '@rspress/core/theme-original';
import { Steps } from './Steps';
import './home.css';

export * from '@rspress/core/theme-original';

export function Layout() {
  return <BasicLayout afterHero={<Steps />} />;
}
