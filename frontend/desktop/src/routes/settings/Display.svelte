<script lang="ts">
  import Card from '../../lib/ui/Card.svelte';
  import Row from '../../lib/ui/Row.svelte';
  import Toggle from '../../lib/ui/Toggle.svelte';
  import type { SettingsView } from '../../mygo';
  import { setTheme } from '../../lib/theme';

  interface Props {
    data: SettingsView;
    save: (key: string, value: string) => void | Promise<void>;
  }
  let { data, save }: Props = $props();

  const s = $derived(data.settings);
  const nightOff = $derived(s['night_off'] === 'true');

  // The theme is not just a stored key: this window and the panel behind it
  // both repaint from the shared store, so it goes through setTheme rather
  // than the generic save().
  let error = $state('');
  async function chooseTheme(value: string) {
    error = '';
    try {
      await setTheme(value as 'system' | 'light' | 'dark');
    } catch (err) {
      error = err instanceof Error ? err.message : String(err);
    }
  }
</script>

<div class="stack">
  {#if error}<p class="error" role="alert">{error}</p>{/if}
  <Card title="显示">
    <Row label="夜间熄屏" hint="到点后屏幕休眠，早上再亮">
      <Toggle checked={nightOff} label="夜间熄屏" onchange={(v) => save('night_off', String(v))} />
    </Row>
    {#if nightOff}
      <Row label="熄屏时间" hint="24 小时制">
        <div class="pair">
          <input
            type="time"
            aria-label="夜间熄屏开始"
            value={s['night_start'] || '23:00'}
            onchange={(e) => save('night_start', e.currentTarget.value)}
          />
          <span class="dash">到</span>
          <input
            type="time"
            aria-label="夜间熄屏结束"
            value={s['night_end'] || '07:00'}
            onchange={(e) => save('night_end', e.currentTarget.value)}
          />
        </div>
      </Row>
    {/if}
    <Row label="主题" hint="跟随系统、浅色或深色">
      <select
        aria-label="主题"
        value={s['theme'] || 'system'}
        onchange={(e) => void chooseTheme(e.currentTarget.value)}
      >
        <option value="system">跟随系统</option>
        <option value="light">浅色</option>
        <option value="dark">深色</option>
      </select>
    </Row>
    <Row label="色盲模式" hint="失败状态画成 ✕，不只靠颜色">
      <Toggle
        checked={s['color_blind'] === 'true'}
        label="色盲模式"
        onchange={(v) => save('color_blind', String(v))}
      />
    </Row>
  </Card>

  <Card title="声音">
    <Row label="扬声器音量">
      <div class="slider">
        <input
          type="range"
          aria-label="扬声器音量"
          min="0"
          max="100"
          step="5"
          value={s['sound_volume'] || '60'}
          onchange={(e) => save('sound_volume', e.currentTarget.value)}
        />
        <span class="value">{s['sound_volume'] || '60'}</span>
      </div>
    </Row>
    <Row label="按键音" hint="按板子上的键时响一下">
      <Toggle
        checked={s['sound_keys'] !== 'false'}
        label="按键音"
        onchange={(v) => save('sound_keys', String(v))}
      />
    </Row>
    <Row label="到点提醒" hint="事项开始时响">
      <Toggle
        checked={s['sound_alarm'] !== 'false'}
        label="到点提醒"
        onchange={(v) => save('sound_alarm', String(v))}
      />
    </Row>
    <Row label="提前提醒" hint="事项开始前也响一次">
      <select
        aria-label="提前提醒"
        value={s['alarm_lead_minutes'] || '0'}
        onchange={(e) => save('alarm_lead_minutes', e.currentTarget.value)}
      >
        <option value="0">不提前</option>
        <option value="5">提前 5 分钟</option>
        <option value="10">提前 10 分钟</option>
        <option value="15">提前 15 分钟</option>
      </select>
    </Row>
  </Card>
</div>

<style>
  .stack {
    display: flex;
    flex-direction: column;
    gap: 14px;
  }
  select {
    height: var(--ctl);
    padding: 0 6px;
    border: 1px solid var(--line);
    border-radius: 7px;
    background: var(--card);
    color: var(--fg);
    font: inherit;
    font-size: 12px;
  }
  input[type='time'] {
    height: var(--ctl);
    padding: 0 4px;
    border: 1px solid var(--line);
    border-radius: 7px;
    background: var(--card);
    color: var(--fg);
    font: inherit;
    font-size: 12px;
  }
  .pair {
    display: flex;
    align-items: center;
    gap: 6px;
  }
  .dash {
    color: var(--muted);
    font-size: 11.5px;
  }
  .slider {
    display: flex;
    align-items: center;
    gap: 8px;
  }
  input[type='range'] {
    width: 130px;
    accent-color: var(--fg);
  }
  .value {
    font-size: 11.5px;
    color: var(--muted);
    font-variant-numeric: tabular-nums;
    width: 22px;
    text-align: right;
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
