<script lang="ts">
  /** A switch: 34×20 track, knob on the right when on (design system). */
  interface Props {
    checked: boolean;
    label: string;
    disabled?: boolean;
    onchange?: (checked: boolean) => void;
  }
  let { checked, label, disabled = false, onchange }: Props = $props();
</script>

<button
  type="button"
  class="toggle"
  class:on={checked}
  role="switch"
  aria-checked={checked}
  aria-label={label}
  {disabled}
  onclick={() => onchange?.(!checked)}
>
  <span class="knob"></span>
</button>

<style>
  .toggle {
    width: 34px;
    height: 20px;
    flex: none;
    padding: 0;
    border: 0;
    border-radius: 999px;
    background: var(--faint);
    position: relative;
    cursor: default;
    transition: background var(--dur-color, 0.12s) var(--ease, ease);
  }
  /* The accent belongs to status and reminders; a switch that is simply on
     is ink, and the knob is the surface colour so it reads in both themes. */
  .toggle.on {
    background: var(--fg);
  }
  .knob {
    position: absolute;
    top: 2px;
    left: 2px;
    width: 16px;
    height: 16px;
    border-radius: 999px;
    background: var(--card);
    box-shadow: 0 1px 2px rgb(0 0 0 / 22%);
    transition: transform var(--dur-seg, 0.18s) var(--ease, ease);
  }
  .toggle.on .knob {
    transform: translateX(14px);
  }
  .toggle:disabled {
    opacity: 0.45;
    cursor: default;
  }
</style>
