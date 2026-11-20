<script lang="ts">
  import Card from '../../lib/ui/Card.svelte';
  import Row from '../../lib/ui/Row.svelte';
  import Toggle from '../../lib/ui/Toggle.svelte';
  import type { SettingsView } from '../../mygo';

  interface Props {
    data: SettingsView;
    save: (key: string, value: string) => void | Promise<void>;
  }
  let { data, save }: Props = $props();

  const s = $derived(data.settings);
  const on = (key: string) => s[key] === 'true';
</script>

<div class="stack">
  <Card title="常规">
    <Row label="开机自启" hint="登录这台 Mac 后自动打开">
      <Toggle
        checked={on('open_at_login')}
        label="开机自启"
        onchange={(v) => save('open_at_login', String(v))}
      />
    </Row>
    <Row label="菜单栏显示" hint="进度、下一项或仅图标">
      <select
        aria-label="菜单栏显示"
        value={s['tray_mode'] || 'progress'}
        onchange={(e) => save('tray_mode', e.currentTarget.value)}
      >
        <option value="progress">进度</option>
        <option value="next">下一项</option>
        <option value="icon">仅图标</option>
      </select>
    </Row>
    <Row label="时区" hint="今天从几点开始算">
      <select
        aria-label="时区"
        value={s['timezone'] || ''}
        onchange={(e) => save('timezone', e.currentTarget.value)}
      >
        {#each data.zones as zone (zone)}
          <option value={zone}>{zone}</option>
        {/each}
      </select>
    </Row>
    <Row label="自动把未完成带到今天" hint="每天第一次打开时">
      <Toggle
        checked={on('auto_carry')}
        label="自动把未完成带到今天"
        onchange={(v) => save('auto_carry', String(v))}
      />
    </Row>
  </Card>

  {#if s['weather_city'] !== undefined}
    <Card title="天气">
      <Row label="默认城市" hint="没开定位时用它">
        <input
          type="text"
          aria-label="默认城市"
          value={s['weather_city'] ?? ''}
          placeholder="北京"
          onchange={(e) => save('weather_city', e.currentTarget.value)}
        />
      </Row>
      <Row label="WeatherAPI 密钥" hint={data.settings['weather_api_key'] || '未设置'}>
        <input
          type="text"
          aria-label="WeatherAPI 密钥"
          value=""
          placeholder={s['weather_api_key'] || '粘贴密钥'}
          onchange={(e) => save('weather_api_key', e.currentTarget.value)}
        />
      </Row>
      <Row label="IP 兜底" hint="定位失败时按 IP 猜城市">
        <Toggle
          checked={s['weather_ip_fallback'] !== 'false'}
          label="IP 兜底"
          onchange={(v) => save('weather_ip_fallback', String(v))}
        />
      </Row>
    </Card>
  {/if}
</div>

<style>
  .stack {
    display: flex;
    flex-direction: column;
    gap: 14px;
  }
  input,
  select {
    height: 25px;
    max-width: 180px;
    padding: 0 6px;
    border: 1px solid var(--line);
    border-radius: 7px;
    background: var(--card);
    color: var(--fg);
    font: inherit;
    font-size: 12px;
  }
  input:focus,
  select:focus {
    outline: none;
    border-color: var(--accent);
    box-shadow: 0 0 0 3px var(--accent-soft);
  }
</style>
