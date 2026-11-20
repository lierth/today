import { writable, get } from 'svelte/store';
import { Panel, type Prefs } from '../mygo';
import { isMyGo, onPreferences, preferences } from 'mygo-runtime';

export type ThemeChoice = 'system' | 'light' | 'dark';

/**
 * The shell's theme, shared by the panel and the settings window. The stored
 * value is "system", "light" or "dark"; the one applied is resolved against the
 * desktop's appearance, so "system" follows the Mac without a reload.
 */
export const theme = writable<ThemeChoice>('system');

/** Color-blind mode turns failed states from a red dot into a drawn ✕. */
export const colorBlind = writable(false);

const darkQuery =
  typeof window === 'undefined' ? null : window.matchMedia('(prefers-color-scheme: dark)');

function apply(choice: ThemeChoice) {
  const dark = choice === 'dark' || (choice === 'system' && (darkQuery?.matches ?? false));
  document.documentElement.dataset.theme = dark ? 'dark' : 'light';
}

/**
 * startTheme keeps the store and the document in step, and follows the Mac.
 *
 * The caller passes the value it read from the app, but the window's controls
 * write through the store afterwards, so the subscribe below is what actually
 * repaints when a select changes — set() alone would leave the document on the
 * old theme until something else re-rendered.
 */
export function startTheme(initial: ThemeChoice) {
  theme.set(initial);
  apply(initial);
  darkQuery?.addEventListener('change', () => apply(get(theme)));
  return theme.subscribe(apply);
}

/**
 * setTheme writes the choice everywhere it is observed: the store (which paints
 * the document), the panel behind this window, and the server.
 */
export async function setTheme(next: ThemeChoice): Promise<void> {
  const previous = get(theme);
  theme.set(next);
  try {
    await Panel.setSetting('theme', next);
  } catch (err) {
    theme.set(previous);
    throw err;
  }
}

/**
 * watchAccessibility mirrors the desktop's reduce-transparency switch onto the
 * root element. The panel asks mygo for a translucent material; when the person
 * has asked the Mac for less transparency, the pages paint an opaque surface
 * instead so the panel does not turn into mud.
 */
export function watchAccessibility() {
  if (typeof window === 'undefined') return;
  const apply = (reduce: boolean) => {
    document.documentElement.dataset.opaque = reduce ? 'true' : 'false';
  };
  if (!isMyGo()) return;
  apply(preferences().reduceTransparency);
  return onPreferences((p) => apply(p.reduceTransparency));
}

/** loadPrefs reads the theme and color-blind switch from the app. */
export async function loadPrefs(): Promise<Prefs | null> {
  if (!isMyGo()) return null;
  try {
    const prefs = await Panel.prefs();
    colorBlind.set(prefs.color_blind);
    theme.set((prefs.theme as ThemeChoice) || 'system');
    return prefs;
  } catch {
    return null;
  }
}
