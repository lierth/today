import { useEffect, useState } from 'react';
import { api } from './api';

export type ThemeChoice = 'system' | 'light' | 'dark';

const darkQuery = () => window.matchMedia('(prefers-color-scheme: dark)');

/**
 * The theme lives on <html data-theme> because every colour in tokens.css and
 * shadcn's variables hang off it. "system" follows the OS and keeps following
 * it while the page is open.
 */
export function applyTheme(choice: ThemeChoice) {
  const root = document.documentElement;
  const apply = () => {
    const dark = choice === 'dark' || (choice === 'system' && darkQuery().matches);
    root.dataset['theme'] = dark ? 'dark' : 'light';
  };
  apply();
  const media = darkQuery();
  media.addEventListener('change', apply);
  return () => media.removeEventListener('change', apply);
}

/**
 * One copy of the choice for the whole page.
 *
 * The sidebar and the settings page both show the theme control. With a
 * useState per call site they were two independent copies: changing it in one
 * left the other showing the old value until a reload. A module-level store
 * with subscribers keeps every control in step, and one loader reads the value
 * from the server.
 */
let current: ThemeChoice = 'system';
let loaded = false;
const listeners = new Set<(next: ThemeChoice) => void>();

function publish(next: ThemeChoice) {
  current = next;
  for (const listener of listeners) listener(next);
}

function subscribe(listener: (next: ThemeChoice) => void) {
  listeners.add(listener);
  listener(current);
  return () => {
    listeners.delete(listener);
  };
}

/** loadThemeOnce asks the server what the person chose, once per page. */
async function loadThemeOnce() {
  if (loaded) return;
  loaded = true;
  try {
    const account = await api.account();
    publish(account.theme);
  } catch {
    // Signed out: the login page answers instead.
    loaded = false;
  }
}

export function useTheme() {
  const [theme, setTheme] = useState<ThemeChoice>(current);

  useEffect(() => subscribe(setTheme), []);
  useEffect(() => {
    void loadThemeOnce();
  }, []);
  useEffect(() => applyTheme(theme), [theme]);

  const save = async (next: ThemeChoice) => {
    const previous = current;
    publish(next);
    try {
      await api.setTheme(next);
    } catch {
      publish(previous); // the control must not lie about what stuck
    }
  };

  return { theme, save };
}
