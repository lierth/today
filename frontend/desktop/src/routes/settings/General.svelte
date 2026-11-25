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
  <Card>
    <Row label="登录时启动">
      <Toggle
        checked={on('open_at_login')}
        label="登录时启动"
        onchange={(v) => save('open_at_login', String(v))}
      />
    </Row>
    <Row label="菜单栏显示">
      <select
        aria-label="菜单栏显示"
        value={s['tray_mode'] || 'icon'}
        onchange={(e) => save('tray_mode', e.currentTarget.value)}
      >
        <option value="icon">仅图标</option>
        <option value="progress">进度</option>
        <option value="next">下一项</option>
      </select>
    </Row>
    <Row label="时区">
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
    <Row label="自动顺延未完成事项" hint="每日首次打开时">
      <Toggle
        checked={on('auto_carry')}
        label="自动顺延未完成事项"
        onchange={(v) => save('auto_carry', String(v))}
      />
    </Row>
  </Card>

  {#if s['weather_city'] !== undefined}
    <Card title="天气">
      <Row label="默认城市">
        <input
          type="text"
          aria-label="默认城市"
          value={s['weather_city'] ?? ''}
          placeholder="北京"
          onchange={(e) => save('weather_city', e.currentTarget.value)}
        />
      </Row>
      <Row label="WeatherAPI 密钥">
        <input
          type="text"
          aria-label="WeatherAPI 密钥"
          value=""
          placeholder={s['weather_api_key'] || '粘贴密钥'}
          onchange={(e) => save('weather_api_key', e.currentTarget.value)}
        />
      </Row>
      <Row label="定位失败时使用 IP 城市">
        <Toggle
          checked={s['weather_ip_fallback'] !== 'false'}
          label="定位失败时使用 IP 城市"
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
    gap: 12px;
  }
  input,
  select {
    height: var(--ctl);
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
