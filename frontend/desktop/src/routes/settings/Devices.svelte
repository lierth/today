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
  let busy = $state(false);
  let newID = $state('');
  let newName = $state('');
  let error = $state('');
  let setup = $state<SetupLines | null>(null);
  let preview = $state('');
  let previewOf = $state('');
  let setupFor = $state('');

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

  async function add() {
    if (!newID.trim()) return;
    await run(async () => {
      await Panel.addDevice(
        newID.trim().toLowerCase(),
        newName.trim() || newID.trim().toLowerCase(),
      );
      adding = false;
      newID = '';
      newName = '';
      await reload();
    });
  }

  async function token(device: DeviceRow) {
    if (
      (device.online || device.firmware || setupFor === device.id) &&
      !window.confirm(`重新生成口令后，${device.name} 需要重新配对。继续？`)
    )
      return;
    await run(async () => {
      setup = await Panel.deviceToken(device.id);
      setupFor = device.id;
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
      if (setupFor === device.id) {
        setup = null;
        setupFor = '';
      }
      await reload();
    });
  }
</script>

<fieldset class="stack" disabled={busy} aria-busy={busy}>
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
          <button type="button" onclick={() => token(device)}>生成配对口令</button>
          <button type="button" class="danger" onclick={() => revoke(device)}>取消配对</button>
        </div>
        {#if previewOf === device.id && preview}
          <img class="frame" src={preview} alt="{device.name} 的画面预览" />
        {/if}
        {#if setupFor === device.id && setup}
          {@const pairing = setup}
          <div class="setup">
            <p>
              长按 KEY 3 秒打开配网热点，在配网页填写服务器地址、设备 ID
              和配对口令。口令只显示这一次。下面的命令用于 USB 串口配置（{pairing.hint}）。
            </p>
            <button type="button" onclick={() => run(() => Panel.copyText(pairing.token))}>
              复制配对口令
            </button>
            {#each pairing.lines as line (line)}
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
  .device {
    display: flex;
    flex-direction: column;
    gap: 5px;
    padding: 14px;
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
