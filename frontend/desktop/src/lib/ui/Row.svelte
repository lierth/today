<script lang="ts">
  import type { Snippet } from 'svelte';
  /** One row of a list card: 40 px tall, label left, control right. */
  interface Props {
    label?: string;
    hint?: string;
    /** stacked puts the hint under the label instead of after it. */
    stacked?: boolean;
    children?: Snippet;
    onclick?: () => void;
  }
  let { label = '', hint = '', stacked = true, children, onclick }: Props = $props();
</script>

{#if onclick}
  <button type="button" class="row" class:stacked {onclick}>
    <span class="text">
      <span class="label">
        {label}{#if !stacked && hint}<span class="hint">{hint}</span>{/if}
      </span>
      {#if stacked && hint}<span class="hint">{hint}</span>{/if}
    </span>
    {@render children?.()}
  </button>
{:else}
  <div class="row" class:stacked>
    <span class="text">
      <span class="label">
        {label}{#if !stacked && hint}<span class="hint">{hint}</span>{/if}
      </span>
      {#if stacked && hint}<span class="hint">{hint}</span>{/if}
    </span>
    {@render children?.()}
  </div>
{/if}

<style>
  .row {
    display: flex;
    align-items: center;
    gap: 10px;
    min-height: var(--row, 40px);
    padding: 10px 14px;
    border: 0;
    border-top: 1px solid var(--line-2);
    background: transparent;
    color: inherit;
    font: inherit;
    text-align: left;
    width: 100%;
  }
  button.row {
    cursor: pointer;
  }
  button.row:hover {
    background: var(--pill);
  }
  .row:first-child {
    border-top: 0;
  }
  .text {
    display: flex;
    flex-direction: column;
    min-width: 0;
    flex: 1;
    gap: 1px;
  }
  .label {
    display: flex;
    align-items: baseline;
    gap: 6px;
    min-width: 0;
    font-size: 12.5px;
    font-weight: 500;
  }
  .hint {
    color: var(--muted);
    font-size: 11.5px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .stacked .text {
    gap: 2px;
  }
  .stacked .hint {
    white-space: normal;
  }
</style>
