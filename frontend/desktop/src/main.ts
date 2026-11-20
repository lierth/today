import { mount } from 'svelte';
import App from './App.svelte';
import Settings from './routes/Settings.svelte';
import '@today/shared/tokens.css';
import './app.css';

// Two windows share one bundle: mygo serves index.html for any path without an
// extension, so the path picks which window this is. The panel sizes itself to
// its content (FitContent); the settings window is a normal resizable window.
const page = window.location.pathname.replace(/\/index\.html$/, '');
const target = document.getElementById('app')!;

// The panel and the settings window want different measurements (a 40 px row
// on glass vs a 48 px row on a solid window), so the stylesheet is told which
// one this is before the app paints.
if (page === '/settings') document.documentElement.dataset.window = 'settings';

const app = mount(page === '/settings' ? Settings : App, { target });

export default app;
