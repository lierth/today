<script lang="ts">
  /**
   * A status dot. A failure always draws a ✕ next to the red — colour alone is
   * not an answer, and the ✕ was previously only drawn for people who had
   * turned on colour-blind mode, so everyone else had to infer it from hue.
   */
  interface Props {
    tone?: 'ok' | 'warn' | 'bad' | 'idle';
    /** Kept so callers need not change; a failure draws the ✕ either way. */
    colorBlind?: boolean;
    label?: string;
  }
  // colorBlind is still accepted so callers need no change, but it is not
  // destructured: the ✕ is drawn for every failure, not only for people who
  // turned colour-blind mode on.
  let { tone = 'idle', label = '' }: Props = $props();
</script>

<span class="dot {tone}" class:blind={tone === 'bad'} role="img" aria-label={label}>
  {#if tone === 'bad'}
    <svg viewBox="0 0 8 8" aria-hidden="true"><path d="M1.6 1.6 6.4 6.4M6.4 1.6 1.6 6.4" /></svg>
  {/if}
</span>

<style>
  .dot {
    width: 6px;
    height: 6px;
    border-radius: 999px;
    flex: none;
    display: inline-grid;
    place-items: center;
    background: var(--faint);
  }
  .dot.ok {
    background: var(--success);
  }
  .dot.warn {
    background: var(--warning);
  }
  .dot.bad {
    background: var(--error);
  }
  /* A dot of 6 px has no room for a legible ✕, so a failure becomes the ✕
     itself: 8 px, no fill, the same red stroke. */
  .dot.blind {
    width: 8px;
    height: 8px;
    background: transparent;
  }
  .dot.blind svg {
    width: 8px;
    height: 8px;
    overflow: visible;
    stroke: var(--error);
    stroke-width: 1.6;
    stroke-linecap: round;
    fill: none;
  }
</style>
