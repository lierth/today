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
  let firmwarePath = $state('');
  let firmwareVersion = $state('');
  $effect(() => {
    if (!data.backups.some((backup) => backup.name === picked)) {
      picked = data.backups[0]?.name ?? '';
    }
  });

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

  async function removeBackup() {
    const name = picked || data.backups[0]?.name;
    if (!name || busy) return;
    busy = true;
    error = '';
    try {
      if (await Panel.deleteBackup(name)) {
        picked = '';
        note = '已删除备份';
        await reload();
      }
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
      if (path.endsWith('-full.bin')) {
        error = '合并镜像仅用于 USB 刷机，请选择不带 -full 的应用固件。';
        return;
      }
      firmwarePath = path;
      const name = path.split('/').pop() ?? '';
      firmwareVersion = name.replace(/^today-(?:rlcd-)?/, '').replace(/\.bin$/, '');
    } catch (err) {
      error = err instanceof Error ? err.message : String(err);
    }
  }

  async function uploadFirmware() {
    busy = true;
    error = '';
    try {
      const staged = await Panel.uploadFirmware(firmwarePath, firmwareVersion);
      note = `已上传 ${staged}，点击推送升级后发送给设备。`;
      firmwarePath = '';
      await reload();
    } catch (err) {
      error = err instanceof Error ? err.message : String(err);
    } finally {
      busy = false;
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
    <Row label="保留时间" hint="自动保留最近 7 天，旧备份会自动删除" stacked />
    <Row label="上次备份" hint={data.last_local || '还没有备份'} stacked />
    {#if data.cloud_on}
      <Row label="iCloud" hint={data.last_cloud || '还没有同步到 iCloud'} stacked />
    {/if}
    {#if data.backup_error}<Row label="备份出错" hint={data.backup_error} stacked />{/if}
  </Card>

  <Card title="恢复">
    <Row
      label="选择备份"
      hint={data.backups.length ? '恢复前会再问一次' : '还没有备份，请先创建一份'}
    >
      <select
        aria-label="选择备份"
        bind:value={picked}
        disabled={busy || data.backups.length === 0}
      >
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
      <button
        type="button"
        class="btn danger"
        onclick={removeBackup}
        disabled={busy || data.backups.length === 0}
      >
        删除备份
      </button>
    </Row>
  </Card>

  <Card title="固件升级">
    <Row label="当前提供的版本" hint={data.firmware || '尚未上传固件'} />
    <Row label="选择应用固件" hint={firmwarePath.split('/').pop() || '使用不带 -full 的 .bin 文件'}>
      <button type="button" class="btn" onclick={chooseFirmware} disabled={busy}>选择文件</button>
    </Row>
    {#if firmwarePath}
      <Row label="固件版本" hint="与这份固件的版本保持一致">
        <input aria-label="固件版本" bind:value={firmwareVersion} placeholder="0.2.0" />
        <button
          type="button"
          class="btn"
          onclick={uploadFirmware}
          disabled={busy || !firmwareVersion.trim()}
        >
          {busy ? '上传中…' : '上传'}
        </button>
      </Row>
    {/if}
    <Row
      label="推送到显示屏"
      hint={data.settings['firmware_pending_version']
        ? `待推送 ${data.settings['firmware_pending_version']}，升级后设备会自动重启`
        : '先选择并上传固件，再推送升级'}
    >
      <button
        type="button"
        class="btn"
        onclick={pushFirmware}
        disabled={busy || !data.settings['firmware_pending_version']}
      >
        推送升级
      </button>
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
  input,
  select {
    height: var(--ctl);
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
    display: inline-flex;
    align-items: center;
    justify-content: center;
    gap: 6px;
    height: var(--ctl);
    padding: 0 10px;
    border: 1px solid var(--line);
    border-radius: 7px;
    background: var(--card);
    color: var(--fg);
    font: inherit;
    font-size: 11.5px;
    cursor: default;
  }
  .btn:hover:not(:disabled) {
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
