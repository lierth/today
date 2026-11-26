<script lang="ts">
  /**
   * Devices: one row per board with its state, and the small set of actions
   * that only make sense with the board in hand.
   */
  import { onMount, tick } from 'svelte';
  import { Panel, type Board, type DeviceRow } from '../../mygo';
  import Card from '../../lib/ui/Card.svelte';
  import Dot from '../../lib/ui/Dot.svelte';
  import type { SettingsView } from '../../mygo';
  import { colorBlind } from '../../lib/theme';

  interface Props {
    data: SettingsView;
    reload: () => void | Promise<void>;
  }
  let { data, reload }: Props = $props();

  let busy = $state(false);
  let error = $state('');
  let preview = $state('');
  let soundSent = $state('');
  let previewOf = $state('');
  let nearby = $state<Board[]>([]);
  let scanning = $state(false);
  let scanError = $state('');
  let selected = $state<Board | null>(null);
  let pairCode = $state('');
  let pairName = $state('');
  let codeInput = $state<HTMLInputElement | undefined>();
  const available = $derived(
    nearby.filter((board) => board.pairable || !data.devices.some((d) => d.id === board.id)),
  );

  async function discover() {
    if (scanning || busy) return;
    scanning = true;
    try {
      nearby = await Panel.discoverBoards();
      scanError = '';
    } catch (err) {
      scanError = err instanceof Error ? err.message : String(err);
    } finally {
      scanning = false;
    }
  }

  onMount(() => {
    const timer = setInterval(() => {
      if (busy) return;
      void reload();
    }, 3000);
    return () => clearInterval(timer);
  });

  async function choose(board: Board) {
    selected = board;
    pairCode = '';
    pairName = data.devices.find((device) => device.id === board.id)?.name ?? '';
    error = '';
    await tick();
    codeInput?.focus();
  }

  /** Every one-shot call goes through here so a failure is shown, not lost. */
  async function run(job: () => Promise<unknown>) {
    if (busy) return;
    busy = true;
    error = '';
    try {
      await job();
    } catch (err) {
      error = err instanceof Error ? err.message : String(err);
    } finally {
      busy = false;
    }
  }

  async function pair(event: SubmitEvent) {
    event.preventDefault();
    const board = selected;
    if (!board || !/^[0-9]{6}$/.test(pairCode)) return;
    await run(async () => {
      await Panel.pairBoard(board.id, pairCode, pairName.trim());
      nearby = nearby.filter((item) => item.id !== board.id);
      selected = null;
      pairCode = '';
      await reload();
    });
  }

  async function audition(id: string) {
    await run(async () => {
      await Panel.previewSound(id);
      soundSent = id;
      setTimeout(() => {
        if (soundSent === id) soundSent = '';
      }, 4000);
    });
  }
  async function frame(id: string) {
    if (previewOf === id) {
      preview = '';
      previewOf = '';
      return;
    }
    await run(async () => {
      preview = await Panel.previewFrame(id, 1);
      previewOf = id;
    });
  }

  async function revoke(device: DeviceRow) {
    if (!window.confirm(`取消配对后，${device.name} 将无法连接。需要重新配对才能恢复。继续？`))
      return;
    await run(async () => {
      await Panel.revokeDevice(device.id);
      await reload();
    });
  }
</script>

<fieldset class="stack" disabled={busy} aria-busy={busy}>
  {#if error || scanError}<p class="error" role="alert">{error || scanError}</p>{/if}

  <Card>
    <div class="discovery-head">
      <h2>附近的显示屏</h2>
      <button type="button" class="plain" disabled={scanning} onclick={discover}>
        {scanning ? '搜索中…' : '刷新'}
      </button>
    </div>
    {#each available as board (board.id)}
      <div class="nearby">
        <span class="name">{board.name}</span>
        {#if board.pairable}<button type="button" onclick={() => choose(board)}>连接</button>
        {:else}<span class="state">已配对 · BOOT 3 秒重新配对</span>{/if}
      </div>
      {#if selected?.id === board.id}
        <form class="form pairing-form" onsubmit={pair}>
          <label>
            配对码 <input
              bind:this={codeInput}
              aria-label="屏幕配对码"
              inputmode="numeric"
              autocomplete="one-time-code"
              maxlength="7"
              pattern={'[0-9]{6}'}
              required
              placeholder="屏幕上的 6 位数字"
              value={pairCode}
              oninput={(event) =>
                (pairCode = event.currentTarget.value.replace(/\D/g, '').slice(0, 6))}
            />
          </label>
          <label>
            名称 <input bind:value={pairName} maxlength="40" placeholder={board.name} />
          </label>
          <div class="form-actions">
            <button type="submit" disabled={!/^[0-9]{6}$/.test(pairCode)}>
              {busy ? '正在配对…' : '配对'}
            </button>
            <button type="button" class="plain" onclick={() => (selected = null)}>取消</button>
          </div>
        </form>
      {/if}
    {/each}
    {#if !available.length}<p class="empty">
        {scanning ? '正在搜索局域网中的显示屏…' : '点击刷新，查找局域网中的显示屏。'}
      </p>{/if}
  </Card>

  {#if data.devices.length > 0}<Card title="已配对">
      {#each data.devices as device (device.id)}
        <div class="device">
          <div class="head">
            <Dot
              tone={device.online ? 'ok' : 'bad'}
              colorBlind={$colorBlind}
              label={`${device.name}${device.online ? '在线' : '离线'} · ${device.since}`}
            />
            <span class="name">{device.name}</span>
            {#if !device.id.startsWith('today-')}<span class="id">{device.id}</span>{/if}
            <span class="state">{device.since}</span>
          </div>
          <div class="meta">
            {#if device.battery}<span>电量 {device.battery}</span>{/if}
            {#if device.climate}<span>{device.climate}</span>{/if}
            {#if device.firmware}<span>固件 {device.firmware}</span>{/if}
            {#if device.city}<span>天气 {device.city}</span>{/if}
          </div>
          <div class="actions">
            <button type="button" onclick={() => run(() => Panel.refreshDevice(device.id))}>
              立即刷新
            </button>
            <button type="button" onclick={() => audition(device.id)}>
              {soundSent === device.id ? '已发送' : '试听提示音'}
            </button>
            <button type="button" onclick={() => frame(device.id)}>
              {previewOf === device.id ? '收起预览' : '布局预览'}
            </button>
            <button type="button" class="danger" onclick={() => revoke(device)}>取消配对</button>
          </div>
          {#if previewOf === device.id && preview}
            <img class="frame" src={preview} alt="{device.name} 的布局预览，时钟和状态为示意值" />
          {/if}
        </div>
      {/each}
    </Card>{/if}
</fieldset>

<style>
  .stack {
    margin: 0;
    padding: 0;
    border: 0;
    min-width: 0;
    display: flex;
    flex-direction: column;
    gap: 14px;
  }
  .discovery-head,
  .nearby {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
    padding: 10px 12px;
  }
  .discovery-head h2 {
    margin: 0;
    color: var(--muted);
    font-size: 11.5px;
    font-weight: 560;
  }
  .nearby {
    border-top: 1px solid var(--line-2);
    min-height: 48px;
  }
  .pairing-form {
    border-top: 1px solid var(--line-2);
    background: var(--card-2);
  }
  .device {
    display: flex;
    flex-direction: column;
    gap: 5px;
    padding: 12px;
    border-top: 1px solid var(--line-2);
  }
  .device:first-child {
    border-top: 0;
  }
  .head {
    display: flex;
    align-items: center;
    gap: 7px;
  }
  .name {
    font-weight: 500;
  }
  .id {
    font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
    font-size: 11px;
    color: var(--muted);
  }
  .state {
    margin-left: auto;
    font-size: 11.5px;
    color: var(--muted);
  }
  .meta {
    display: flex;
    flex-wrap: wrap;
    gap: 10px;
    font-size: 11.5px;
    color: var(--muted);
    font-variant-numeric: tabular-nums;
  }
  .actions {
    display: flex;
    flex-wrap: wrap;
    gap: 6px;
  }
  button {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    gap: 6px;
    height: var(--ctl);
    padding: 0 9px;
    border: 1px solid var(--line);
    border-radius: 7px;
    background: var(--card);
    color: var(--fg);
    font: inherit;
    font-size: 11.5px;
    cursor: default;
    box-shadow: 0 1px 1px rgb(20 20 30 / 4%);
  }
  button:hover:not(:disabled) {
    background: var(--pill);
  }
  button.danger:hover {
    color: var(--error);
    background: var(--error-soft);
  }
  button:disabled {
    opacity: 0.5;
    cursor: default;
  }
  .frame {
    width: 100%;
    max-width: 400px;
    border-radius: 8px;
    border: 1px solid var(--line);
    image-rendering: pixelated;
  }
  .empty {
    margin: 0;
    padding: 12px 12px 14px;
    color: var(--muted);
    font-size: 12px;
  }
  .form {
    display: flex;
    flex-direction: column;
    gap: 8px;
    padding: 12px;
  }
  .form label {
    display: flex;
    align-items: center;
    gap: 8px;
    font-size: 12px;
    color: var(--fg-2);
  }
  .form input {
    flex: 1;
    height: var(--ctl);
    padding: 0 7px;
    border: 1px solid var(--line);
    border-radius: 7px;
    background: var(--card);
    color: var(--fg);
    font: inherit;
    font-size: 12px;
  }
  .form-actions {
    display: flex;
    gap: 6px;
  }
  .plain {
    background: transparent;
  }
  .error {
    margin: 0;
    color: var(--error);
    font-size: 12px;
  }
</style>
