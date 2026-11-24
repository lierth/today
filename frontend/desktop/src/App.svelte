<script lang="ts">
  /**
   * The tray panel: the day, the rows, one quick-add field, and a footer with
   * the boards and the weather. It is the panel mygo anchors under the tray
   * icon (see host.togglePanel), so it sizes itself and never scrolls as a
   * whole — only the list does.
   */
  import { Panel, type TodayView } from './mygo';
  import Snail from './Snail.svelte';
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

  $effect(() => {
    void (async () => {
      watchAccessibility();
      startTheme('system');
      await loadPrefs();
      await load();
    })();
  });

  const progress = $derived(view ? `${view.done}/${view.total}` : '');
</script>

<main>
  <header>
    <div class="brand"><Snail size={20} /><span>today</span></div>
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
        <svg viewBox="0 0 16 16" aria-hidden="true">
          <path
            d="M6.5 1.5h3l.5 2 1.5.9 2-.5 1.5 2.6-1.5 1.4v1.7l1.5 1.4-1.5 2.6-2-.5-1.5.9-.5 2h-3l-.5-2-1.5-.9-2 .5L1 11l1.5-1.4V7.9L1 6.5l1.5-2.6 2 .5L6 3.5Z"
          /><circle cx="8" cy="8" r="2.3" />
        </svg>
      </button>
    </div>
  </header>

  <section class="overview" aria-label="今日概览">
    <div class="day">
      <h1>{view?.date ?? '今天'} <span>{view?.weekday ?? ''}</span></h1>
      <span class="progress">{progress ? `已完成 ${progress}` : ''}</span>
    </div>
    <p class="subtitle">
      {view?.lunar ?? '把今天，慢慢做好。'}{#if view?.weather}<span> · {view.weather}</span>{/if}
    </p>
    <div
      class="track"
      role="progressbar"
      aria-label="今天的进度"
      aria-valuemin="0"
      aria-valuemax={Math.max(1, view?.total ?? 0)}
      aria-valuenow={view?.done ?? 0}
    >
      <span style:width={`${view?.total ? (view.done / view.total) * 100 : 0}%`}></span>
    </div>
  </section>

  {#if !live}<p class="hint">请从 Mac 菜单栏打开 Today。</p>{/if}
  {#if view && view.carried.length > 0}
    <div class="carry">
      <span>之前还有 {view.carried.length} 项未完成</span><button type="button" onclick={carryAll}>
        移到今天
      </button>
    </div>
  {/if}

  <div class="scroll">
    <Card>
      {#if loading}
        <div class="skeleton" aria-label="正在读取事项">
          <span></span><span></span><span></span>
        </div>
      {:else if view && view.items.length === 0}
        <div class="empty">
          <Snail size={28} />
          <p>今天，从一件小事开始。</p>
          <span>在下面写一行，回车添加。</span>
        </div>
      {:else}
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
      {/if}
    </Card>
  </div>

  <QuickAdd {busy} placeholder="写一行，回车添加…" onsubmit={add} />
  {#if error}<p class="error" role="alert">{error}</p>{/if}

  <footer>
    {#if (view?.devices ?? []).length > 0}
      <div class="devices">
        {#each view?.devices ?? [] as device (device.id)}
          <button
            type="button"
            class="device"
            onclick={() => openSettings('devices')}
            title="查看设备设置"
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
      </div>
    {:else}<button type="button" class="link" onclick={() => openSettings('devices')}>
        连接桌上的显示屏
      </button>{/if}
    <button type="button" class="open-web" onclick={() => live && Panel.openPage('/')}>
      <span>打开完整日程</span><span aria-hidden="true">↗</span>
    </button>
  </footer>
</main>

<style>
  main {
    display: flex;
    flex-direction: column;
    gap: 12px;
    width: 360px;
    padding: 16px;
  }
  header,
  .brand,
  .actions,
  .day {
    display: flex;
    align-items: center;
  }
  header,
  .day {
    justify-content: space-between;
    gap: 8px;
  }
  .brand {
    gap: 7px;
    font-size: 13px;
    font-weight: 600;
    letter-spacing: 0.01em;
  }
  .actions {
    gap: 3px;
  }
  .icon {
    width: 27px;
    height: 27px;
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
    fill: none;
    stroke: currentcolor;
    stroke-width: 1.3;
    stroke-linecap: round;
    stroke-linejoin: round;
  }
  .overview {
    padding: 2px 0 3px;
  }
  h1 {
    margin: 0;
    font-size: 21px;
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
    font-size: 11px;
    color: var(--ctl-fg);
    font-variant-numeric: tabular-nums;
    white-space: nowrap;
  }
  .subtitle {
    margin: 3px 0 12px;
    font-size: 11px;
    line-height: 1.5;
    color: var(--muted);
  }
  .track {
    height: 3px;
    overflow: hidden;
    border-radius: 3px;
    background: var(--line);
  }
  .track span {
    display: block;
    height: 100%;
    background: var(--accent);
    border-radius: inherit;
    transition: width 0.2s var(--ease);
  }
  .scroll {
    max-height: 320px;
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
    color: var(--fg);
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
    min-height: 42px;
    padding: 0 12px;
    border-top: 1px solid var(--line-2);
  }
  .item:first-child {
    border-top: 0;
  }
  .label {
    flex: 1;
    min-width: 0;
    padding: 10px 0;
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
  .empty {
    padding: 24px 12px;
    text-align: center;
    color: var(--muted);
  }
  .empty :global(svg) {
    display: block;
    margin: 0 auto 10px;
    color: var(--fg-2);
  }
  .empty p {
    margin: 0 0 4px;
    color: var(--fg-2);
    font-size: 12px;
  }
  .empty span {
    font-size: 11px;
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
    flex-direction: column;
    gap: 10px;
    font-size: 11px;
    color: var(--muted);
  }
  .devices {
    border-top: 1px solid var(--line);
    padding-top: 6px;
  }
  .device {
    display: flex;
    align-items: center;
    gap: 6px;
    width: 100%;
    padding: 6px 0;
    border: 0;
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
  }
  .open-web {
    display: flex;
    justify-content: space-between;
    padding: 8px 10px;
    border: 1px solid var(--line);
    border-radius: 7px;
    background: var(--card);
    color: var(--ctl-fg);
    font: inherit;
    cursor: default;
    box-shadow: 0 1px 1px rgb(20 20 30 / 4%);
  }
  .open-web:hover {
    color: var(--fg);
    border-color: var(--muted);
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
