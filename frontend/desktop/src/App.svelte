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
    if (!view) return;
    try {
      view = await toggleDone(view, id, done);
      error = '';
    } catch (err) {
      error = err instanceof Error ? err.message : String(err);
      await load();
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
    await Panel.refreshAll();
    await load();
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
    <Snail size={20} />
    <div class="title">
      <div class="line">
        <span class="name">today</span>
        <span class="date">{view?.date ?? ''}</span>
        <span class="weekday">{view?.weekday ?? ''}</span>
      </div>
      <div class="sub">{view?.lunar ?? ''}</div>
    </div>
    <span class="progress" aria-label="今天的进度">{progress}</span>
    <button type="button" class="icon" aria-label="刷新" title="刷新" onclick={refresh}>
      <svg viewBox="0 0 16 16" aria-hidden="true">
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
          d="M8 10.1a2.1 2.1 0 1 0 0-4.2 2.1 2.1 0 0 0 0 4.2ZM12.9 9.6l1 .8-1.2 2-1.2-.5a4.7 4.7 0 0 1-1.2.7l-.2 1.3H6.9l-.2-1.3a4.7 4.7 0 0 1-1.2-.7l-1.2.5-1.2-2 1-.8a4.8 4.8 0 0 1 0-1.4l-1-.8 1.2-2 1.2.5a4.7 4.7 0 0 1 1.2-.7l.2-1.3h2.2l.2 1.3a4.7 4.7 0 0 1 1.2.7l1.2-.5 1.2 2-1 .8a4.8 4.8 0 0 1 0 1.4Z"
        />
      </svg>
    </button>
  </header>

  {#if !live}
    <p class="hint">
      这个页面要在 Today 的窗口里打开：<code>pnpm dev</code> 单独跑时没有 mygo 绑定。
    </p>
  {/if}

  {#if view && view.carried.length > 0}
    <div class="carry">
      <span>之前还有 {view.carried.length} 项没完成</span>
      <button type="button" onclick={carryAll}>全部移到今天</button>
    </div>
  {/if}

  <div class="scroll">
    <Card>
      {#if loading}
        <div class="skeleton">
          <span></span><span></span><span></span>
        </div>
      {:else if view && view.items.length === 0}
        <p class="empty">今天还没有事项。在下面写一行，今天就算开始了。</p>
      {:else}
        {#each view?.items ?? [] as item (item.id)}
          <div class="item" class:done={item.done}>
            <Check
              checked={item.done}
              label={item.title}
              onchange={(next) => toggle(item.id, next)}
            />
            <span class="label" title={carriedDay(view?.carried ?? [], item.id) || item.title}>
              {item.title}
            </span>
            {#if item.time}<span class="time">{item.time}</span>{/if}
          </div>
        {/each}
      {/if}
    </Card>
  </div>

  <QuickAdd {busy} onsubmit={add} />

  {#if error}<p class="error" role="alert">{error}</p>{/if}

  <footer>
    <div class="devices">
      {#each view?.devices ?? [] as device (device.id)}
        <span class="device" title="{device.name} · {device.since}">
          <Dot
            tone={device.online ? 'ok' : 'bad'}
            colorBlind={$blind}
            label={`${device.name}${device.online ? '在线' : '离线'} · ${device.since}`}
          />
          <span class="who">{device.name}</span>
          {#if device.battery}<span class="meta">{device.battery}</span>{/if}
          {#if device.climate}<span class="meta">{device.climate}</span>{/if}
        </span>
      {/each}
      {#if (view?.devices ?? []).length === 0}
        <button type="button" class="link" onclick={() => openSettings('devices')}>
          添加一台设备
        </button>
      {/if}
    </div>
    <span class="weather">{view?.weather ?? ''}</span>
  </footer>
</main>

<style>
  main {
    display: flex;
    flex-direction: column;
    gap: 10px;
    /* FitContent sizes the window from this box: the width is fixed so the
       panel does not wobble as titles change, the height follows the rows. */
    width: 340px;
    box-sizing: border-box;
    padding: 12px 12px 10px;
  }
  header {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 0 2px;
  }
  header :global(svg) {
    color: var(--fg);
  }
  .title {
    flex: 1;
    min-width: 0;
  }
  .line {
    display: flex;
    align-items: baseline;
    gap: 6px;
  }
  .name {
    font-size: 13px;
    font-weight: 600;
    letter-spacing: 0.01em;
  }
  .date {
    font-size: 12px;
    color: var(--fg-2);
  }
  .weekday {
    font-size: 12px;
    color: var(--muted);
  }
  .sub {
    font-size: 11px;
    color: var(--muted);
    font-variant-numeric: tabular-nums;
  }
  .progress {
    font-size: 11.5px;
    color: var(--muted);
    font-variant-numeric: tabular-nums;
  }
  .icon {
    width: var(--ctl, 26px);
    height: var(--ctl, 26px);
    display: grid;
    place-items: center;
    padding: 0;
    border: 0;
    border-radius: 6px;
    background: transparent;
    color: var(--ctl-fg);
    cursor: pointer;
  }
  .icon:hover {
    background: var(--pill);
    color: var(--fg);
  }
  .icon svg {
    width: 14px;
    height: 14px;
    fill: none;
    stroke: currentcolor;
    stroke-width: 1.4;
    stroke-linecap: round;
    stroke-linejoin: round;
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
    padding: 0 4px;
    font-size: 11.5px;
    color: var(--muted);
  }
  /* Accent is for status and reminders. A text action is ink with an
     underline on hover, so a page of links does not read as a page of alerts. */
  .carry button {
    border: 0;
    background: transparent;
    padding: 0;
    color: var(--fg);
    font: inherit;
    text-decoration: underline;
    text-decoration-color: var(--line);
    text-underline-offset: 2px;
    cursor: pointer;
  }
  .carry button:hover {
    text-decoration-color: var(--fg);
  }
  .item {
    display: flex;
    align-items: center;
    gap: 10px;
    min-height: var(--row, 40px);
    padding: 0 12px 0 14px;
    border-top: 1px solid var(--line-2);
  }
  .item:first-child {
    border-top: 0;
  }
  .label {
    flex: 1;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .item.done .label {
    color: var(--faint);
    text-decoration: line-through;
    text-decoration-color: var(--faint);
  }
  .time {
    font-size: 11.5px;
    color: var(--muted);
    font-variant-numeric: tabular-nums;
  }
  .empty {
    margin: 0;
    padding: 14px 14px 16px;
    color: var(--muted);
    font-size: 12px;
  }
  .skeleton {
    display: flex;
    flex-direction: column;
    gap: 8px;
    padding: 12px 14px 14px;
  }
  .skeleton span {
    height: 10px;
    border-radius: 6px;
    background: var(--pill);
    animation: pulse 1.4s ease-in-out infinite;
  }
  .skeleton span:nth-child(2) {
    width: 70%;
  }
  .skeleton span:nth-child(3) {
    width: 45%;
  }
  @keyframes pulse {
    50% {
      opacity: 0.5;
    }
  }
  .error {
    margin: 0;
    padding: 0 4px;
    color: var(--error);
    font-size: 11.5px;
  }
  footer {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 10px;
    padding: 0 4px;
    font-size: 11px;
    color: var(--muted);
  }
  .devices {
    display: flex;
    gap: 10px;
    min-width: 0;
    overflow: hidden;
  }
  .device {
    display: inline-flex;
    align-items: center;
    gap: 4px;
    min-width: 0;
  }
  .who {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .meta {
    font-variant-numeric: tabular-nums;
    color: var(--faint);
  }
  .link {
    border: 0;
    background: transparent;
    padding: 0;
    font: inherit;
    color: var(--fg);
    text-decoration: underline;
    text-decoration-color: var(--line);
    text-underline-offset: 2px;
    cursor: pointer;
  }
  .link:hover {
    text-decoration-color: var(--fg);
  }
  .weather {
    flex: none;
    font-variant-numeric: tabular-nums;
  }
  .hint {
    margin: 0;
    padding: 0 4px;
    color: var(--muted);
    font-size: 11.5px;
  }
  code {
    font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
    font-size: 11px;
  }
</style>
