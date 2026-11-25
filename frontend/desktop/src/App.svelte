<script lang="ts">
  /**
   * The tray panel: the date, quick add, rows, and compact footer actions.
   * It is the panel mygo anchors under the tray
   * icon (see host.togglePanel), so it sizes itself and never scrolls as a
   * whole — only the list does.
   */
  import { Panel, type TodayView } from './mygo';
  import { onMount } from 'svelte';
  import Card from './lib/ui/Card.svelte';
  import Check from './lib/ui/Check.svelte';
  import Dot from './lib/ui/Dot.svelte';
  import QuickAdd from './lib/ui/QuickAdd.svelte';
  import { colorBlind, loadPrefs, startTheme, watchAccessibility } from './lib/theme';
  import { carriedDay, carryToday, live, toggleDone } from './lib/panel';

  let view = $state<TodayView | null>(null);
  let error = $state('');
  let busy = $state(false);
  let loading = $state(true);
  let refreshing = $state(false);
  let keyboardNavigation = $state(false);
  let pending = $state<string[]>([]);

  const blind = colorBlind;

  async function load() {
    if (!live) {
      loading = false;
      return;
    }
    try {
      view = await Panel.today();
      error = '';
    } catch (err) {
      error = err instanceof Error ? err.message : String(err);
    } finally {
      loading = false;
    }
  }

  async function toggle(id: string, done: boolean) {
    if (!view || pending.includes(id)) return;
    pending = [...pending, id];
    try {
      view = await toggleDone(view, id, done);
      error = '';
    } catch (err) {
      error = err instanceof Error ? err.message : String(err);
      await load();
    } finally {
      pending = pending.filter((value) => value !== id);
    }
  }

  async function add(text: string) {
    busy = true;
    try {
      await Panel.add(text);
      await load();
      error = '';
    } catch (err) {
      error = err instanceof Error ? err.message : String(err);
      throw err;
    } finally {
      busy = false;
    }
  }

  async function carryAll() {
    if (!view) return;
    try {
      view = await carryToday('');
      error = '';
    } catch (err) {
      error = err instanceof Error ? err.message : String(err);
    }
  }

  function openSettings(tab = '') {
    if (live) void Panel.openSettings(tab);
  }

  async function refresh() {
    if (!live || refreshing) return;
    refreshing = true;
    try {
      await Panel.refreshAll();
      await load();
    } catch (err) {
      error = err instanceof Error ? err.message : String(err);
    } finally {
      refreshing = false;
    }
  }

  async function reload() {
    await loadPrefs();
    await load();
  }

  function onPanelFocus() {
    keyboardNavigation = false;
    if (!loading) void reload();
  }

  onMount(() => {
    const stopAccessibility = watchAccessibility();
    const stopTheme = startTheme('system');
    void reload();
    return () => {
      stopAccessibility?.();
      stopTheme();
    };
  });

  const progress = $derived(view ? `${view.done}/${view.total}` : '');
</script>

<svelte:window
  onfocus={onPanelFocus}
  onblur={() => (keyboardNavigation = false)}
  onpointerdown={() => (keyboardNavigation = false)}
  onkeydown={(event) => {
    if (event.key === 'Tab') keyboardNavigation = true;
  }}
/>

<main class:keyboard={keyboardNavigation}>
  <section class="overview" aria-label="今日概览">
    <header>
      <h1>{view?.date ?? '今天'} <span>{view?.weekday ?? ''}</span></h1>
      <div class="actions">
        <button
          type="button"
          class="icon"
          aria-label="刷新"
          title="刷新"
          disabled={refreshing}
          onclick={refresh}
        >
          <svg class:spinning={refreshing} viewBox="0 0 16 16" aria-hidden="true">
            <path d="M13 8a5 5 0 1 1-1.6-3.7M13 2.6V5.2h-2.6" />
          </svg>
        </button>
        <button
          type="button"
          class="icon"
          aria-label="设置"
          title="设置"
          onclick={() => openSettings()}
        >
          <svg viewBox="0 0 24 24" aria-hidden="true">
            <path
              d="m9.5 3-.6 2.3-1.6.9L5 5.6 2.5 10l1.7 1.7v1.8L2.5 15 5 19.4l2.3-.6 1.6.9.6 2.3h5l.6-2.3 1.6-.9 2.3.6 2.5-4.4-1.7-1.5v-1.8l1.7-1.7L19 5.6l-2.3.6-1.6-.9L14.5 3Z"
            /><circle cx="12" cy="12.5" r="3.2" />
          </svg>
        </button>
      </div>
    </header>

    <p class="subtitle">
      <span>{view?.lunar ?? ''}</span>{#if view?.weather}<span class="weather">
          {view.weather}
        </span>{/if}
      {#if view?.total}<span
          class="progress"
          aria-label={`已完成 ${view.done} 项，共 ${view.total} 项`}
        >
          {progress}
        </span>{/if}
    </p>
    {#if view?.total}
      <div
        class="track"
        role="progressbar"
        aria-label="今天的进度"
        aria-valuemin="0"
        aria-valuemax={view.total}
        aria-valuenow={view.done}
      >
        <span style:width={`${(view.done / view.total) * 100}%`}></span>
      </div>
    {/if}
  </section>

  <QuickAdd {busy} placeholder="添加事项…" onsubmit={add} />

  {#if !live}<p class="hint">请从 Mac 菜单栏打开 Today。</p>{/if}
  {#if view && view.carried.length > 0}
    <div class="carry">
      <span>之前还有 {view.carried.length} 项未完成</span><button type="button" onclick={carryAll}>
        移到今天
      </button>
    </div>
  {/if}

  {#if loading}
    <Card>
      <div class="skeleton" aria-label="正在读取事项">
        <span></span><span></span><span></span>
      </div>
    </Card>
  {:else if view?.items.length}
    <div class="scroll">
      <Card>
        {#each view?.items ?? [] as item (item.id)}
          <div class="item" class:done={item.done}>
            <Check
              checked={item.done}
              disabled={pending.includes(item.id)}
              label={`${item.done ? '标为未完成' : '标为完成'}：${item.title}`}
              onchange={(next) => toggle(item.id, next)}
            />
            <button
              type="button"
              class="label"
              aria-label={`编辑 ${item.title}`}
              title={carriedDay(view?.carried ?? [], item.id) || item.title}
              onclick={() => live && Panel.openPage(`/?item=${encodeURIComponent(item.id)}`)}
            >
              {item.title}
            </button>
            {#if item.pinned}<svg class="pin" viewBox="0 0 16 16" role="img" aria-label="已置顶">
                <path d="M5 2h6l-1 4 3 3v1H9v4l-2-2v-2H3V9l3-3Z" />
              </svg>{/if}
            {#if item.time}<span class="time">{item.time}</span>{/if}
          </div>
        {/each}
      </Card>
    </div>
  {/if}

  {#if error}<p class="error" role="alert">{error}</p>{/if}

  <footer>
    <div class="devices">
      {#if (view?.devices ?? []).length > 0}
        {#each view?.devices ?? [] as device (device.id)}
          <button
            type="button"
            class="device"
            onclick={() => openSettings('devices')}
            title={[
              device.name,
              device.online ? '在线' : '离线',
              device.battery,
              device.climate,
              device.since,
            ]
              .filter(Boolean)
              .join(' · ')}
          >
            <Dot
              tone={device.online ? 'ok' : 'bad'}
              colorBlind={$blind}
              label={device.online ? '在线' : '离线'}
            />
            <span class="who">{device.name}</span>
            <span class="meta">
              {[device.battery, device.climate].filter(Boolean).join(' · ') || device.since}
            </span>
          </button>
        {/each}
      {:else}<button type="button" class="link" onclick={() => openSettings('devices')}>
          <svg viewBox="0 0 16 16" aria-hidden="true">
            <rect x="1.5" y="2.5" width="13" height="9" rx="1.5" /><path d="M5.5 14h5M8 11.5V14" />
          </svg>
          连接显示屏
        </button>{/if}
    </div>
    <button type="button" class="open-web" onclick={() => live && Panel.openPage('/')}>
      <span>完整日程</span><svg viewBox="0 0 16 16" aria-hidden="true">
        <path d="M4 12 12 4M5 4h7v7" />
      </svg>
    </button>
  </footer>
</main>

<style>
  /* WebKit can restore keyboard focus when a mouse-opened flyout activates. */
  main:not(.keyboard) :global(button:focus-visible) {
    outline: none;
  }
  main {
    display: flex;
    flex-direction: column;
    gap: 10px;
    width: 360px;
    max-width: 100vw;
    padding: 12px;
  }
  header,
  .actions {
    display: flex;
    align-items: center;
  }
  header {
    justify-content: space-between;
    gap: 8px;
  }
  .actions {
    gap: 3px;
  }
  .icon {
    width: 26px;
    height: 26px;
    display: grid;
    place-items: center;
    padding: 0;
    border: 0;
    border-radius: 7px;
    background: transparent;
    color: var(--ctl-fg);
    cursor: default;
  }
  .icon:hover {
    background: var(--pill);
    color: var(--fg);
  }
  .icon:disabled {
    opacity: 0.5;
  }
  .icon svg {
    width: 15px;
    height: 15px;
    flex: none;
    fill: none;
    stroke: currentcolor;
    stroke-width: 1.3;
    stroke-linecap: round;
    stroke-linejoin: round;
  }
  .icon[aria-label='设置'] svg {
    width: 16px;
    height: 16px;
    stroke-width: 1.7;
  }
  .overview {
    padding: 0 2px;
  }
  h1 {
    margin: 0;
    font-size: 18px;
    line-height: 24px;
    font-weight: 600;
    letter-spacing: -0.035em;
  }
  h1 span {
    margin-left: 3px;
    font-size: 12px;
    font-weight: 400;
    letter-spacing: 0;
    color: var(--muted);
  }
  .progress {
    margin-left: auto;
    font-size: 11px;
    color: var(--ctl-fg);
    font-variant-numeric: tabular-nums;
    white-space: nowrap;
  }
  .subtitle {
    display: flex;
    align-items: center;
    gap: 8px;
    margin: 3px 0 0;
    font-size: 11px;
    line-height: 16px;
    color: var(--muted);
  }
  .weather {
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .track {
    height: 1px;
    margin-top: 8px;
    overflow: hidden;
    border-radius: 3px;
    background: var(--line-2);
  }
  .track span {
    display: block;
    height: 100%;
    background: var(--accent);
    border-radius: inherit;
    transition: width 0.2s var(--ease);
  }
  .scroll {
    max-height: 280px;
    overflow-y: auto;
    border-radius: 12px;
  }
  .carry {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 8px;
    font-size: 11.5px;
    color: var(--ctl-fg);
  }
  .carry button,
  .link {
    border: 0;
    background: transparent;
    padding: 0;
    color: var(--ctl-fg);
    font: inherit;
    cursor: default;
  }
  .carry button:hover,
  .link:hover {
    text-decoration: underline;
    text-underline-offset: 3px;
  }
  .item {
    display: flex;
    align-items: center;
    gap: 10px;
    min-height: 40px;
    padding: 0 10px;
    border-top: 1px solid var(--line-2);
  }
  .item:first-child {
    border-top: 0;
  }
  .label {
    flex: 1;
    min-width: 0;
    padding: 9px 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    text-align: left;
    border: 0;
    background: transparent;
    color: var(--fg);
    font: inherit;
    cursor: default;
  }
  .label:hover {
    color: var(--accent);
  }
  .item.done .label {
    color: var(--muted);
    text-decoration: line-through;
    text-decoration-color: var(--faint);
  }
  .time,
  .pin {
    font-size: 11px;
    color: var(--muted);
    font-variant-numeric: tabular-nums;
  }
  .skeleton {
    display: flex;
    flex-direction: column;
    gap: 12px;
    padding: 16px 12px;
  }
  .skeleton span {
    height: 12px;
    border-radius: 4px;
    background: var(--pill);
  }
  .skeleton span:nth-child(2) {
    width: 70%;
  }
  .skeleton span:nth-child(3) {
    width: 45%;
  }
  .error {
    margin: 0;
    padding: 8px 10px;
    border-radius: 8px;
    background: var(--error-soft);
    color: var(--error);
    font-size: 11.5px;
  }
  footer {
    display: flex;
    align-items: flex-start;
    justify-content: space-between;
    gap: 10px;
    border-top: 1px solid var(--line-2);
    padding-top: 6px;
    font-size: 12px;
    color: var(--muted);
  }
  .devices {
    flex: 1;
    min-width: 0;
  }
  .device {
    display: flex;
    align-items: center;
    gap: 6px;
    width: 100%;
    min-height: 28px;
    padding: 4px 8px;
    border: 0;
    border-radius: var(--radius);
    background: transparent;
    color: var(--ctl-fg);
    font: inherit;
    text-align: left;
    cursor: default;
  }
  .who {
    flex: 1;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .meta {
    font-size: 10.5px;
    font-variant-numeric: tabular-nums;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .link,
  .open-web {
    display: flex;
    align-items: center;
    gap: 6px;
    min-height: 28px;
    padding: 4px 6px;
    border: 0;
    border-radius: var(--radius-control);
    background: transparent;
    color: var(--ctl-fg);
    font: inherit;
    cursor: default;
    white-space: nowrap;
  }
  .link svg,
  .open-web svg {
    width: 13px;
    height: 13px;
    fill: none;
    stroke: currentcolor;
    stroke-width: 1.3;
    stroke-linecap: round;
    stroke-linejoin: round;
  }
  .link:hover,
  .device:hover,
  .open-web:hover {
    color: var(--fg);
    background: var(--pill);
  }
  .hint {
    margin: 0;
    color: var(--muted);
    font-size: 11.5px;
  }
  .spinning {
    animation: spin 0.8s linear infinite;
  }
  @keyframes spin {
    to {
      transform: rotate(360deg);
    }
  }
  .pin {
    width: 12px;
    height: 12px;
    fill: none;
    stroke: currentcolor;
    stroke-width: 1.1;
  }
</style>
