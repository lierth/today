<script lang="ts">
  import Card from '../../lib/ui/Card.svelte';
  import Row from '../../lib/ui/Row.svelte';
  import type { SettingsView } from '../../mygo';
  import { Panel } from '../../mygo';

  interface Props {
    data: SettingsView;
    reload: () => void | Promise<void>;
  }
  let { data, reload }: Props = $props();

  let picked = $state('');
  let busy = $state(false);
  let error = $state('');
  let note = $state('');

  async function backupNow() {
    busy = true;
    error = '';
    try {
      await Panel.backupNow();
      note = '已备份';
      await reload();
      setTimeout(() => (note = ''), 2000);
    } catch (err) {
      error = err instanceof Error ? err.message : String(err);
    } finally {
      busy = false;
    }
  }

  async function restore() {
    const name = picked || data.backups[0]?.name;
    if (!name) return;
    busy = true;
    error = '';
    try {
      const done = await Panel.confirmRestore(name);
      if (done) {
        note = '已恢复';
        await reload();
      }
    } catch (err) {
      error = err instanceof Error ? err.message : String(err);
    } finally {
      busy = false;
    }
  }

  async function pushFirmware() {
    busy = true;
    error = '';
    try {
      note = await Panel.pushFirmware();
      await reload();
    } catch (err) {
      error = err instanceof Error ? err.message : String(err);
    } finally {
      busy = false;
    }
  }

  async function chooseFirmware() {
    error = '';
    try {
      const path = await Panel.chooseFirmware();
      if (!path) return;
      const version =
        path
          .split('/')
          .pop()
          ?.replace(/^today-/, '')
          .replace(/\.bin$/, '') ?? '';
      const staged = await Panel.uploadFirmware(path, version);
      note = '固件已上传：' + staged;
      await reload();
    } catch (err) {
      error = err instanceof Error ? err.message : String(err);
    }
  }
</script>

<div class="stack">
  {#if error}<p class="error" role="alert">{error}</p>{/if}
  {#if note}<p class="note">{note}</p>{/if}

  <Card title="备份">
    <Row label="现在备份一次" hint="写到本机；开了 iCloud 再抄一份">
      <button type="button" class="btn" onclick={backupNow} disabled={busy}>备份</button>
    </Row>
    <Row label="上次备份" hint={data.last_local || '还没有备份'} stacked />
    {#if data.cloud_on}
      <Row label="iCloud" hint={data.last_cloud || '还没有同步到 iCloud'} stacked />
    {/if}
    {#if data.backup_error}<Row label="备份出错" hint={data.backup_error} stacked />{/if}
  </Card>

  <Card title="恢复">
    <Row label="选择备份" hint="恢复前会再问一次">
      <select aria-label="选择备份" bind:value={picked}>
        {#each data.backups as b (b.name)}
          <option value={b.name}>{b.when} · {b.where} · {b.size}</option>
        {/each}
      </select>
    </Row>
    <Row label="" hint="恢复会覆盖现在的事项，并让本机登录失效；恢复前的数据会另存。">
      <button
        type="button"
        class="btn danger"
        onclick={restore}
        disabled={busy || data.backups.length === 0}
      >
        恢复
      </button>
    </Row>
  </Card>

  <Card title="固件">
    <Row label="当前固件" hint={data.firmware || '设备还没上报过'} stacked />
    <Row label="上传固件" hint="选一个 .bin，板子下次取帧后升级">
      <button type="button" class="btn" onclick={chooseFirmware} disabled={busy}>选择文件</button>
    </Row>
    <Row label="推给设备" hint="已上传但没推的时候用">
      <button type="button" class="btn" onclick={pushFirmware} disabled={busy}>推送</button>
    </Row>
  </Card>

  <Card title="数据位置">
    <Row label={data.data_root} hint={data.data_note || '数据库与备份都放在这里'} stacked />
  </Card>
</div>

<style>
  .stack {
    display: flex;
    flex-direction: column;
    gap: 14px;
  }
  select {
    height: 25px;
    max-width: 260px;
    padding: 0 6px;
    border: 1px solid var(--line);
    border-radius: 7px;
    background: var(--card);
    color: var(--fg);
    font: inherit;
    font-size: 12px;
  }
  .btn {
    height: 24px;
    padding: 0 10px;
    border: 1px solid var(--line);
    border-radius: 7px;
    background: var(--card);
    color: var(--fg);
    font: inherit;
    font-size: 11.5px;
    cursor: pointer;
  }
  .btn:hover {
    background: var(--pill);
  }
  .btn.danger {
    color: var(--error);
  }
  .btn:disabled {
    opacity: 0.5;
    cursor: default;
  }
  .error {
    margin: 0;
    color: var(--error);
    font-size: 12px;
  }
  .note {
    margin: 0;
    color: var(--success);
    font-size: 12px;
  }
</style>
