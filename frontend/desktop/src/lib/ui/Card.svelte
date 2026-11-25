<script lang="ts">
  import type { Snippet } from 'svelte';
  /** A list card: 12 px radius, one hairline around, rows inside. */
  interface Props {
    title?: string;
    children?: Snippet;
  }
  let { title = '', children }: Props = $props();
</script>

<section class="card">
  {#if title}<h2>{title}</h2>{/if}
  {@render children?.()}
</section>

<style>
  /* Cards are flat: the hairline separates them, and only floating layers
     (popovers, the drawer, a dragged row) carry a shadow. */
  /* On the panel's tinted glass a drawn border would double up with the
     material's own edge, so the card keeps only an inset hairline. A settings
     window is opaque and keeps the ordinary 1px border. */
  .card {
    background: var(--card);
    border-radius: var(--radius-surface);
    overflow: hidden;
    box-shadow: inset 0 0 0 1px var(--line-2);
  }
  :global(:root[data-window='settings']) .card {
    box-shadow: none;
    border: 1px solid var(--line);
  }
  :global(:root[data-window='settings']) h2 {
    padding-left: 12px;
    padding-right: 12px;
  }
  h2 {
    margin: 0;
    padding: 10px 14px 6px;
    font-size: 11.5px;
    font-weight: 560;
    color: var(--muted);
    letter-spacing: 0.02em;
    /* Section headings are chrome, not text to copy. */
    user-select: none;
  }
</style>
