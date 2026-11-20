<script lang="ts">
  /**
   * Devices: one row per board with its state, and the small set of actions
   * that only make sense with the board in hand — mint a token, preview the
   * frame, ring it, revoke it.
   */
  import { Panel, type DeviceRow, type SetupLines } from '../../mygo';
  import Card from '../../lib/ui/Card.svelte';
  import Dot from '../../lib/ui/Dot.svelte';
  import type { SettingsView } from '../../mygo';
  import { colorBlind } from '../../lib/theme';

  interface Props {
    data: SettingsView;
    reload: () => void | Promise<void>;
  }
  let { data, reload }: Props = $props();

  let adding = $state(false);
  let newID = $state('');
  let newName = $state('');
  let error = $state('');
  let setup = $state<SetupLines | null>(null);
  let preview = $state('');
  let previewOf = $state('');
  let setupFor = $state('');

  /** Every one-shot call goes through here so a failure is shown, not lost. */
  async function run(job: () => Promise<unknown>) {
    error = '';
    try {
      await job();
    } catch (err) {
      error = err instanceof Error ? err.message : String(err);
    }
  }

  async function add() {
    if (!newID.trim()) return;
    try {
      await Panel.addDevice(
        newID.trim().toLowerCase(),
        newName.trim() || newID.trim().toLowerCase(),
      );
      adding = false;
      newID = '';
      newName = '';
      await reload();
    } catch (err) {
      error = err instanceof Error ? err.message : String(err);
    }
  }

  async function token(id: string) {
    error = '';
    try {
      setup = await Panel.deviceToken(id);
      setupFor = id;
    } catch (err) {
      error = err instanceof Error ? err.message : String(err);
    }
  }

  async function frame(id: string) {
    if (previewOf === id) {
      preview = '';
      previewOf = '';
      return;
    }
    try {
      preview = await Panel.previewFrame(id, 1);
      previewOf = id;
    } catch (err) {
      error = err instanceof Error ? err.message : String(err);
    }
  }

  async function revoke(device: DeviceRow) {
    try {
      await Panel.revokeDevice(device.id);
      await reload();
    } catch (err) {
      error = err instanceof Error ? err.message : String(err);
    }
  }
</script>

<div class="stack">
  {#if error}<p class="error" role="alert">{error}</p>{/if}

  <Card title="设备">
    {#each data.devices as device (device.id)}
      <div class="device">
        <div class="head">
          <Dot
            tone={device.online ? 'ok' : 'bad'}
            colorBlind={$colorBlind}
            label={`${device.name}${device.online ? '在线' : '离线'} · ${device.since}`}
          />
          <span class="name">{device.name}</span>
          <span class="id">{device.id}</span>
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
          <button type="button" onclick={() => run(() => Panel.previewSound(device.id))}>
            试听提示音
          </button>
          <button type="button" onclick={() => frame(device.id)}>
            {previewOf === device.id ? '收起预览' : '预览画面'}
          </button>
          <button type="button" onclick={() => token(device.id)}>生成 token</button>
          <button type="button" class="danger" onclick={() => revoke(device)}>吊销</button>
        </div>
        {#if previewOf === device.id && preview}
          <img class="frame" src={preview} alt="{device.name} 的画面预览" />
        {/if}
        {#if setupFor === device.id && setup}
          <div class="setup">
            <p>
              在板子上长按 KEY 开热点，连上后在配网页逐行执行（token 只显示这一次，{setup.hint}）：
            </p>
            {#each setup.lines as line (line)}
              <button
                type="button"
                class="cmd"
                onclick={() => run(() => Panel.copyText(line))}
                title="点击复制"
              >
                <code>{line}</code>
              </button>
            {/each}
          </div>
        {/if}
      </div>
    {/each}
    {#if data.devices.length === 0}
      <p class="empty">还没有设备。先加一台，再按生成的步骤在板子上配网。</p>
    {/if}
  </Card>

  {#if adding}
    <Card title="新建设备">
      <div class="form">
        <label>ID <input type="text" bind:value={newID} placeholder="dev1" /></label>
        <label>名称 <input type="text" bind:value={newName} placeholder="书桌" /></label>
        <div class="form-actions">
          <button type="button" onclick={add}>添加</button>
          <button type="button" class="plain" onclick={() => (adding = false)}>取消</button>
        </div>
      </div>
    </Card>
  {:else}
    <button type="button" class="add" onclick={() => (adding = true)}>添加一台设备</button>
  {/if}
</div>

<style>
  .stack {
    display: flex;
    flex-direction: column;
    gap: 14px;
  }
  .device {
    display: flex;
    flex-direction: column;
    gap: 5px;
    padding: 10px 14px;
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
    height: 24px;
    padding: 0 9px;
    border: 1px solid var(--line);
    border-radius: 7px;
    background: var(--card);
    color: var(--fg);
    font: inherit;
    font-size: 11.5px;
    cursor: pointer;
    box-shadow: 0 1px 1px rgb(20 20 30 / 4%);
  }
  button:hover {
    background: var(--pill);
  }
  button.danger {
    color: var(--error);
  }
  .frame {
    width: 100%;
    max-width: 400px;
    border-radius: 8px;
    border: 1px solid var(--line);
    image-rendering: pixelated;
  }
  .setup {
    background: var(--card-2);
    border-radius: 8px;
    padding: 8px 10px;
    display: flex;
    flex-direction: column;
    gap: 4px;
  }
  .setup p {
    margin: 0 0 2px;
    font-size: 11.5px;
    color: var(--muted);
  }
  .cmd {
    text-align: left;
    height: auto;
    padding: 3px 7px;
    font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
    font-size: 11.5px;
  }
  .empty {
    margin: 0;
    padding: 12px 14px 14px;
    color: var(--muted);
    font-size: 12px;
  }
  .add {
    align-self: flex-start;
    height: 26px;
  }
  .form {
    display: flex;
    flex-direction: column;
    gap: 8px;
    padding: 12px 14px;
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
    height: 25px;
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
