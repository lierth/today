<script lang="ts">
  import Card from '../../lib/ui/Card.svelte';
  import Row from '../../lib/ui/Row.svelte';
  import Toggle from '../../lib/ui/Toggle.svelte';
  import { Panel, type SettingsView } from '../../mygo';

  interface Props {
    data: SettingsView;
    save: (key: string, value: string) => void | Promise<void>;
  }
  let { data, save }: Props = $props();

  const s = $derived(data.settings);
  const proxyOn = $derived(s['proxy_mode'] === 'on');

  // Both buttons below open something outside the app (Tailscale's setup page,
  // System Settings). They can fail, and a silent dead button is worse than a
  // sentence, so the failure goes to the same alert the other cards use.
  let error = $state('');

  async function open(url: string) {
    error = '';
    try {
      await Panel.openURL(url);
    } catch (err) {
      error = err instanceof Error ? err.message : String(err);
    }
  }

  async function openHelp() {
    error = '';
    try {
      await Panel.localNetworkHelp();
    } catch (err) {
      error = err instanceof Error ? err.message : String(err);
    }
  }
</script>

<div class="stack">
  {#if error}<p class="error" role="alert">{error}</p>{/if}
  <Card title="外网访问">
    <Row
      label="通过 Tailscale Funnel 对外开放"
      hint={data.funnel_detail || '让不在同一 Wi-Fi 的手机也能打开'}
      stacked
    >
      <Toggle
        checked={s['funnel'] === 'true'}
        label="外网访问"
        disabled={data.dev}
        onchange={(v) => save('funnel', String(v))}
      />
    </Row>
    {#if data.funnel_action}
      <Row label="需要继续设置" hint={data.funnel} stacked>
        <button type="button" class="linkbtn" onclick={() => open(data.funnel_action)}>
          打开设置页
        </button>
      </Row>
    {/if}
    <Row
      label="本地网络"
      hint={data.local_net === 'ok'
        ? '已允许，手机可以连入'
        : '被拒绝时手机连不上，需要到系统设置里允许'}
      stacked
    >
      <button type="button" class="linkbtn" onclick={() => openHelp()}>去允许</button>
    </Row>
  </Card>

  <Card title="代理">
    <Row label="走代理" hint="天气预报与检查更新用它">
      <Toggle
        checked={proxyOn}
        label="走代理"
        onchange={(v) => save('proxy_mode', v ? 'on' : 'off')}
      />
    </Row>
    {#if proxyOn}
      <Row label="代理地址" hint="http(s):// 或 socks5://host:port">
        <input
          type="text"
          aria-label="代理地址"
          value={s['proxy_url'] ?? ''}
          placeholder="http://127.0.0.1:7890"
          onchange={(e) => save('proxy_url', e.currentTarget.value)}
        />
      </Row>
    {/if}
    <Row label="当前生效" hint={data.proxy_summary || '直连'} stacked></Row>
  </Card>
</div>

<style>
  .stack {
    display: flex;
    flex-direction: column;
    gap: 14px;
  }
  input {
    height: 25px;
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
  .linkbtn {
    height: 24px;
    padding: 0 9px;
    border: 1px solid var(--line);
    border-radius: 7px;
    background: var(--card);
    color: var(--fg);
    font: inherit;
    font-size: 11.5px;
    cursor: pointer;
  }
  .linkbtn:hover {
    background: var(--pill);
  }
  .error {
    margin: 0;
    padding: 7px 10px;
    border-radius: 8px;
    background: var(--error-soft, var(--pill));
    color: var(--error);
    font-size: 11.5px;
    user-select: text;
  }
</style>
