<script lang="ts">
  /**
   * The settings window: one card per group, a segmented control on top where a
   * toolbar would be. The tab lives in the URL (?tab=devices) so the panel's
   * "添加一台设备" and "设置 → 数据" land in the right place.
   */
  import { isMyGo } from 'mygo-runtime';
  import { Panel, type SettingsView } from '../mygo';
  import Snail from '../Snail.svelte';
  import { loadPrefs, startTheme, watchAccessibility } from '../lib/theme';
  import General from './settings/General.svelte';
  import Devices from './settings/Devices.svelte';
  import Display from './settings/Display.svelte';
  import Network from './settings/Network.svelte';
  import Data from './settings/Data.svelte';
  import About from './settings/About.svelte';

  type Tab = { id: string; label: string };
  const tabs: Tab[] = [
    { id: 'general', label: '常规' },
    { id: 'devices', label: '设备' },
    { id: 'display', label: '显示与声音' },
    { id: 'network', label: '网络' },
    { id: 'data', label: '数据' },
    { id: 'about', label: '关于' },
  ];

  const wanted = new URLSearchParams(window.location.search).get('tab') ?? 'general';
  let tab = $state(tabs.some((t) => t.id === wanted) ? wanted : 'general');
  let data = $state<SettingsView | null>(null);
  let error = $state('');
  let note = $state('');
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
    try {
      await Panel.setSetting(key, value);
      note = '已保存';
      await load();
      setTimeout(() => (note = ''), 1500);
    } catch (err) {
      error = err instanceof Error ? err.message : String(err);
    }
  }

  /** Arrow keys move between tabs and give the new one focus; the roving
      tabindex keeps only the current tab reachable by Tab itself. */
  function onTabKey(event: KeyboardEvent) {
    const keys: Record<string, number> = { ArrowLeft: -1, ArrowRight: 1 };
    const step = keys[event.key];
    let next: number;
    if (step) next = (tabs.findIndex((t) => t.id === tab) + step + tabs.length) % tabs.length;
    else if (event.key === 'Home') next = 0;
    else if (event.key === 'End') next = tabs.length - 1;
    else return;
    event.preventDefault();
    const picked = tabs[next];
    if (!picked) return;
    tab = picked.id;
    const list = (event.currentTarget as HTMLElement).querySelectorAll<HTMLButtonElement>(
      '[role="tab"]',
    );
    list[next]?.focus();
  }

  $effect(() => {
    void (async () => {
      watchAccessibility();
      startTheme('system');
      await loadPrefs();
      await load();
    })();
  });
</script>

<div class="window">
  <header>
    <Snail size={18} />
    <span class="brand">today</span>
    <span class="grow"></span>
    <span class="note" aria-live="polite">{note}</span>
  </header>

  <!-- A tablist is one stop in the tab order: the arrows move between tabs,
       Home/End jump to the ends, exactly as the platform expects. -->
  <div class="tabs" role="tablist" aria-label="设置分组" tabindex="-1" onkeydown={onTabKey}>
    {#each tabs as t (t.id)}
      <button
        type="button"
        role="tab"
        aria-selected={tab === t.id}
        tabindex={tab === t.id ? 0 : -1}
        class:on={tab === t.id}
        onclick={() => (tab = t.id)}
      >
        {t.label}
      </button>
    {/each}
  </div>

  {#if error}<p class="error" role="alert">{error}</p>{/if}

  <div class="body">
    {#if !data}
      <p class="loading">正在读取设置…</p>
    {:else if tab === 'general'}
      <General {data} {save} />
    {:else if tab === 'devices'}
      <Devices {data} reload={load} />
    {:else if tab === 'display'}
      <Display {data} {save} />
    {:else if tab === 'network'}
      <Network {data} {save} />
    {:else if tab === 'data'}
      <Data {data} reload={load} />
    {:else}
      <About {data} {save} reload={load} />
    {/if}
  </div>
</div>

<style>
  .window {
    display: flex;
    flex-direction: column;
    height: 100vh;
    padding: 0 0 14px;
  }
  header {
    display: flex;
    align-items: center;
    gap: 8px;
    /* Room for the traffic lights mygo keeps with TitleBarHiddenInset. */
    padding: calc(var(--mygo-titlebar-height, 28px) + 4px) 18px 6px;
  }
  .brand {
    font-size: 13px;
    font-weight: 600;
  }
  .grow {
    flex: 1;
  }
  .note {
    font-size: 11px;
    color: var(--success);
  }
  .tabs {
    display: flex;
    justify-content: center;
    gap: 2px;
    padding: 0 18px 10px;
  }
  .tabs button {
    height: 26px;
    padding: 0 10px;
    border: 0;
    border-radius: 7px;
    background: transparent;
    color: var(--ctl-fg);
    font: inherit;
    font-size: 12px;
    cursor: pointer;
  }
  .tabs button:hover {
    color: var(--fg);
  }
  .tabs button.on {
    background: var(--card);
    color: var(--fg);
    box-shadow:
      0 0 0 1px var(--line),
      0 1px 2px rgb(20 20 30 / 6%);
  }
  .body {
    flex: 1;
    overflow: auto;
    padding: 0 18px;
  }
  .error {
    margin: 0 18px 8px;
    color: var(--error);
    font-size: 12px;
  }
  .loading {
    color: var(--muted);
    font-size: 12px;
  }
</style>
