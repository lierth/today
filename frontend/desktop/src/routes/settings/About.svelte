<script lang="ts">
  import Card from '../../lib/ui/Card.svelte';
  import Row from '../../lib/ui/Row.svelte';
  import { Panel, type SettingsView } from '../../mygo';
  import Snail from '../../Snail.svelte';

  interface Props {
    data: SettingsView;
    save: (key: string, value: string) => void | Promise<void>;
    reload: () => void | Promise<void>;
  }
  let { data, save, reload }: Props = $props();

  let error = $state('');
  let busy = $state(false);
  let ready = $state('');

  async function check() {
    busy = true;
    error = '';
    try {
      await Panel.checkUpdate();
      await reload();
    } catch (err) {
      error = err instanceof Error ? err.message : String(err);
    } finally {
      busy = false;
    }
  }

  async function download() {
    try {
      await Panel.downloadUpdate();
      // The download runs in the background; poll the same read the card shows.
      setTimeout(() => void reload(), 1200);
    } catch (err) {
      error = err instanceof Error ? err.message : String(err);
    }
  }

  $effect(() => {
    void (async () => {
      try {
        ready = await Panel.pendingUpdateNote();
      } catch {
        ready = '';
      }
    })();
  });

  const update = $derived(data.update);
  const newer = $derived(update.latest !== '' && update.latest !== data.version);
  const s = $derived(data.settings);
</script>

<div class="stack">
  {#if error}<p class="error" role="alert">{error}</p>{/if}

  <div class="hero">
    <Snail size={40} />
    <div class="about">
      <div class="name">Today</div>
      <div class="version">
        {data.version}{#if data.dev}
          · 开发版{/if}
      </div>
    </div>
    <span class="grow"></span>
    <button type="button" class="btn" onclick={check} disabled={busy || update.updating}>
      {busy ? '检查中…' : '检查更新'}
    </button>
  </div>

  <Card title="更新">
    <Row label="上次检查" hint={update.status || '还没检查过'} stacked>
      {#if update.latest}
        <span class="latest">最新 {update.latest}</span>
      {/if}
    </Row>
    {#if newer}
      <Row label="有新版本 {update.latest}" hint="下载后退出 Today 时安装" stacked>
        <button type="button" class="btn primary" onclick={download} disabled={update.updating}>
          {update.updating ? '下载中…' : '下载'}
        </button>
      </Row>
    {/if}
    {#if ready}
      <Row label="更新已下载" hint="退出 Today 时安装，下次打开就是新版" stacked />
    {/if}
    <Row label="更新源" hint="GitHub Releases 或自建地址">
      <input
        type="text"
        aria-label="更新源"
        value={s['update_feed'] ?? ''}
        placeholder="https://api.github.com/repos/owner/repo/releases/latest"
        onchange={(e) => save('update_feed', e.currentTarget.value)}
      />
    </Row>
    <Row label="闲时自动下载" hint="下载在后台，装在你退出时">
      <button
        type="button"
        class="btn"
        aria-pressed={s['update_auto_download'] === 'true'}
        onclick={() => save('update_auto_download', String(s['update_auto_download'] !== 'true'))}
      >
        {s['update_auto_download'] === 'true' ? '已开启' : '已关闭'}
      </button>
    </Row>
  </Card>

  <Card title="关于">
    <Row label="开源">
      <span class="source-url">
        {data.source_url || 'https://github.com/lierth/today'}
      </span>
    </Row>
    <Row label={data.data_root} hint={data.data_note || '数据库与备份都放在这里'} stacked />
  </Card>
</div>

<style>
  .stack {
    display: flex;
    flex-direction: column;
    gap: 14px;
  }
  .source-url {
    color: var(--fg-2);
    font-size: 12px;
    overflow-wrap: anywhere;
    user-select: text;
    cursor: text;
  }
  .hero {
    display: flex;
    align-items: center;
    gap: 14px;
    /* The cards below inset their contents by 14 px; the hero matches that so
       the mark and "Today" sit on the same column as the row labels. */
    padding: 2px 14px 0;
  }
  .hero :global(svg) {
    color: var(--fg);
  }
  .name {
    font-size: 17px;
    font-weight: 600;
    letter-spacing: 0.01em;
  }
  .version {
    font-size: 12px;
    color: var(--muted);
    font-variant-numeric: tabular-nums;
  }
  /* The version is the label of the product, so the update action lives on the
     hero line instead of a card row that repeats the version a second time. */
  .grow {
    flex: 1;
  }
  .latest {
    font-size: 11.5px;
    color: var(--muted);
    font-variant-numeric: tabular-nums;
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
  .btn.primary {
    background: var(--accent);
    border-color: var(--accent);
    color: var(--accent-fg);
  }
  .btn.primary:hover {
    filter: brightness(1.08);
    background: var(--accent);
  }
  .btn:disabled {
    opacity: 0.5;
    cursor: default;
  }
  input {
    height: var(--ctl);
    max-width: 220px;
    padding: 0 6px;
    border: 1px solid var(--line);
    border-radius: 7px;
    background: var(--card);
    color: var(--fg);
    font: inherit;
    font-size: 12px;
  }
  input:focus {
    outline: none;
    border-color: var(--accent);
    box-shadow: 0 0 0 3px var(--accent-soft);
  }
  .error {
    margin: 0;
    color: var(--error);
    font-size: 12px;
  }
</style>
