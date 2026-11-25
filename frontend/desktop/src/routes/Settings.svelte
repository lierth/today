<script lang="ts">
  /**
   * The settings window: compact navigation beside grouped controls.
   * The tab lives in the URL (?tab=devices) so the panel's
   * "添加一台设备" and "设置 → 数据" land in the right place.
   */
  import { isMyGo } from 'mygo-runtime';
  import { onMount, tick } from 'svelte';
  import { Panel, type SettingsView } from '../mygo';
  import { loadPrefs, startTheme, watchAccessibility } from '../lib/theme';
  import General from './settings/General.svelte';
  import Devices from './settings/Devices.svelte';
  import Display from './settings/Display.svelte';
  import Network from './settings/Network.svelte';
  import Data from './settings/Data.svelte';
  import About from './settings/About.svelte';

  type Tab = { id: string; label: string; icon: string };
  const tabs: Tab[] = [
    {
      id: 'general',
      icon: 'M4 7h16M4 17h16M8 4v6M16 14v6',
      label: '常规',
    },
    {
      id: 'devices',
      icon: 'M5 3h14v14H5zM9 21h6M12 17v4',
      label: '设备',
    },
    {
      id: 'display',
      icon: 'M18 8a6 6 0 0 0-12 0c0 7-3 7-3 9h18c0-2-3-2-3-9M10 21h4',
      label: '显示与声音',
    },
    {
      id: 'network',
      icon: 'M2 8a16 16 0 0 1 20 0M5 12a11 11 0 0 1 14 0M9 16a5 5 0 0 1 6 0M12 20h.01',
      label: '网络',
    },
    {
      id: 'data',
      icon: 'M4 6c0-4 16-4 16 0s-16 4-16 0v12c0 4 16 4 16 0V6M4 12c0 4 16 4 16 0',
      label: '数据',
    },
    {
      id: 'about',
      icon: 'M21 12a9 9 0 1 1-18 0 9 9 0 0 1 18 0M12 11v6M12 7h.01',
      label: '关于',
    },
  ];

  const wanted = new URLSearchParams(window.location.search).get('tab') ?? 'general';
  let tab = $state(tabs.some((t) => t.id === wanted) ? wanted : 'general');
  let data = $state<SettingsView | null>(null);
  let error = $state('');
  let note = $state('');
  let saving = $state(0);
  let keyboardNavigation = $state(false);
  let noticeTimer: ReturnType<typeof setTimeout>;
  const selected = $derived(tabs.find((t) => t.id === tab)!);
  const live = isMyGo();

  async function load() {
    if (!live) return;
    try {
      data = await Panel.settingsData();
      error = '';
    } catch (err) {
      error = err instanceof Error ? err.message : String(err);
    }
  }

  /** save writes one key and reloads, so a hook's effect (login item, funnel)
      shows up in the card that owns it. */
  async function save(key: string, value: string) {
    if (!live) return;
    saving++;
    clearTimeout(noticeTimer);
    try {
      await Panel.setSetting(key, value);
      note = '已保存';
      await load();
      noticeTimer = setTimeout(() => (note = ''), 2000);
    } catch (err) {
      error = err instanceof Error ? err.message : String(err);
    } finally {
      saving--;
    }
  }

  function chooseTab(id: string) {
    tab = id;
    history.replaceState(null, '', `?tab=${id}`);
  }

  /** Arrow keys move between tabs and give the new one focus; the roving
      tabindex keeps only the current tab reachable by Tab itself. */
  function onTabKey(event: KeyboardEvent) {
    const keys: Record<string, number> = {
      ArrowLeft: -1,
      ArrowRight: 1,
      ArrowUp: -1,
      ArrowDown: 1,
    };
    const step = keys[event.key];
    let next: number;
    if (step) next = (tabs.findIndex((t) => t.id === tab) + step + tabs.length) % tabs.length;
    else if (event.key === 'Home') next = 0;
    else if (event.key === 'End') next = tabs.length - 1;
    else return;
    event.preventDefault();
    keyboardNavigation = true;
    const picked = tabs[next];
    if (!picked) return;
    chooseTab(picked.id);
    const list = (event.currentTarget as HTMLElement).querySelectorAll<HTMLButtonElement>(
      '[role="tab"]',
    );
    list[next]?.focus();
  }

  onMount(() => {
    const stopAccessibility = watchAccessibility();
    const stopTheme = startTheme('system');
    let active = true;
    void (async () => {
      await loadPrefs();
      await load();
      await tick();
      if (active && live) await Panel.settingsReady();
    })().catch((err) => {
      if (active) error = err instanceof Error ? err.message : String(err);
    });
    return () => {
      active = false;
      stopAccessibility?.();
      stopTheme();
      clearTimeout(noticeTimer);
    };
  });
</script>

<svelte:window
  onfocus={() => (keyboardNavigation = false)}
  onblur={() => (keyboardNavigation = false)}
  onpointerdown={() => (keyboardNavigation = false)}
  onkeydown={(event) => {
    if (event.key === 'Tab') keyboardNavigation = true;
  }}
/>

<div class="window" class:keyboard={keyboardNavigation}>
  <aside>
    <div
      class="tabs"
      role="tablist"
      aria-label="设置分组"
      aria-orientation="vertical"
      tabindex="-1"
      onkeydown={onTabKey}
    >
      {#each tabs as t (t.id)}
        <button
          type="button"
          id={`tab-${t.id}`}
          role="tab"
          aria-selected={tab === t.id}
          aria-controls="settings-content"
          tabindex={tab === t.id ? 0 : -1}
          class:on={tab === t.id}
          onclick={() => chooseTab(t.id)}
        >
          <svg class="nav-mark" viewBox="0 0 24 24" aria-hidden="true">
            <path d={t.icon} />
          </svg>{t.label}
        </button>
      {/each}
    </div>
    <span class="local">Today {data?.version ?? ''}</span>
  </aside>

  <section class="content">
    <header>
      <h1>{selected.label}</h1>
      <span class="note" aria-live="polite">{saving ? '正在保存…' : note}</span>
    </header>
    {#if error}<p class="error" role="alert">{error}</p>{/if}
    <div class="body" role="tabpanel" id="settings-content" aria-labelledby={`tab-${tab}`}>
      {#if !data}
        <div class="loading">
          <p>{error ? '设置暂时无法读取。' : '正在读取设置…'}</p>
          <button type="button" onclick={load}>重新读取</button>
        </div>
      {:else}
        <fieldset disabled={saving > 0} aria-busy={saving > 0}>
          {#if tab === 'general'}<General {data} {save} />
          {:else if tab === 'devices'}<Devices {data} reload={load} />
          {:else if tab === 'display'}<Display {data} {save} />
          {:else if tab === 'network'}<Network {data} {save} />
          {:else if tab === 'data'}<Data {data} reload={load} />
          {:else}<About {data} {save} reload={load} />{/if}
        </fieldset>
      {/if}
    </div>
  </section>
</div>

<style>
  .window:not(.keyboard) :global(button:focus-visible) {
    outline: none;
  }
  .window {
    display: flex;
    height: 100vh;
    overflow: hidden;
  }
  aside,
  .content {
    --app-region: drag;
  }
  .tabs,
  .body,
  .error {
    --app-region: no-drag;
  }
  aside {
    display: flex;
    flex-direction: column;
    flex: 0 0 148px;
    padding: calc(var(--mygo-titlebar-height, 28px) + 8px) 10px 12px;
    border-right: 1px solid var(--line);
    background: color-mix(in oklab, var(--bg) 80%, transparent);
  }
  .tabs {
    display: flex;
    flex-direction: column;
    gap: 3px;
  }
  .tabs button {
    display: flex;
    align-items: center;
    gap: 9px;
    width: 100%;
    height: 32px;
    padding: 0 9px;
    border: 0;
    border-radius: 8px;
    background: transparent;
    color: var(--ctl-fg);
    font: inherit;
    font-size: 12px;
    text-align: left;
    cursor: default;
  }
  .tabs button:hover {
    color: var(--fg);
    background: var(--pill);
  }
  .tabs button.on {
    background: var(--sel);
    color: var(--fg);
    font-weight: 500;
  }
  .nav-mark {
    width: 16px;
    height: 16px;
    flex: none;
    fill: none;
    stroke: currentcolor;
    stroke-width: 1.6;
    stroke-linecap: round;
    stroke-linejoin: round;
  }
  .local {
    margin-top: auto;
    padding: 20px 8px 0;
    font-size: 10px;
    color: var(--muted);
  }
  .content {
    display: flex;
    flex-direction: column;
    flex: 1;
    min-width: 0;
    padding-top: 20px;
  }
  header {
    user-select: none;
    display: flex;
    justify-content: space-between;
    align-items: center;
    gap: 12px;
    min-height: 28px;
    padding: 0 20px 12px;
  }
  h1 {
    margin: 0;
    font-size: 18px;
    font-weight: 600;
    letter-spacing: -0.03em;
  }
  .note {
    font-size: 11px;
    color: var(--ctl-fg);
    white-space: nowrap;
  }
  .body {
    flex: 1;
    min-height: 0;
    overflow: auto;
    padding: 0 20px 20px;
  }
  fieldset {
    min-width: 0;
    margin: 0;
    padding: 0;
    border: 0;
    max-width: 760px;
  }
  .error {
    margin: 0 20px 10px;
    padding: 9px 12px;
    border: 1px solid var(--error);
    border-radius: 8px;
    background: var(--error-soft);
    color: var(--error);
    font-size: 12px;
  }
  .loading {
    padding: 24px 0;
    color: var(--muted);
    font-size: 12px;
  }
  .loading button {
    padding: 6px 10px;
    border: 1px solid var(--line);
    border-radius: 7px;
    background: var(--card);
    color: var(--fg);
    font: inherit;
  }
</style>
