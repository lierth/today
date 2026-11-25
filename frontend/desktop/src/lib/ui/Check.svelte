<script lang="ts">
  /** The one checkbox shape in the whole app: a circle that fills when done. */
  interface Props {
    checked: boolean;
    /** failed draws ✕ in color-blind mode instead of a red mark. */
    failed?: boolean;
    label?: string;
    disabled?: boolean;
    onchange?: (checked: boolean) => void;
  }
  let { checked, failed = false, label = '', disabled = false, onchange }: Props = $props();
</script>

<button
  type="button"
  class="check"
  class:checked
  class:failed
  role="checkbox"
  aria-checked={checked}
  aria-label={label || (checked ? '取消完成' : '标记完成')}
  {disabled}
  onclick={() => onchange?.(!checked)}
>
  {#if checked}
    <svg viewBox="0 0 16 16" aria-hidden="true"><path d="M3.6 8.4 6.5 11.2 12.4 4.8" /></svg>
  {:else if failed}
    <svg viewBox="0 0 16 16" aria-hidden="true">
      <path d="M4.4 4.4 11.6 11.6M11.6 4.4 4.4 11.6" />
    </svg>
  {/if}
</button>

<style>
  .check {
    width: 18px;
    height: 18px;
    flex: none;
    margin: 0;
    padding: 0;
    border-radius: 999px;
    border: 1.5px solid var(--faint);
    background: transparent;
    display: grid;
    place-items: center;
    cursor: default;
    transition:
      background var(--dur-color, 0.12s) var(--ease, ease),
      border-color var(--dur-color, 0.12s) var(--ease, ease);
  }
  .check:hover:not(:disabled) {
    border-color: var(--ctl-fg, #63636b);
  }
  .check:disabled {
    opacity: 0.5;
  }
  .check.checked {
    background: var(--fg);
    border-color: var(--fg);
  }
  .check.checked svg {
    stroke: var(--card);
  }
  .check.failed {
    border-color: var(--error);
  }
  .check.failed svg {
    stroke: var(--error);
  }
  svg {
    width: 12px;
    height: 12px;
    fill: none;
    stroke-width: 2;
    stroke-linecap: round;
    stroke-linejoin: round;
  }
</style>
