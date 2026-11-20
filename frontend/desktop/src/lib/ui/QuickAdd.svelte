<script lang="ts">
  /**
   * The quick-add field: the draft, the chips the parser found under it, and
   * the × that removes a chip by cutting its exact text out of the draft.
   */
  import { Panel, type Tag } from '../../mygo';
  import { cutFragment } from '../panel';
  import { isMyGo } from 'mygo-runtime';

  interface Props {
    onsubmit?: (text: string) => void | Promise<void>;
    placeholder?: string;
    busy?: boolean;
  }
  let { onsubmit, placeholder = '写点什么…', busy = false }: Props = $props();

  let draft = $state('');
  let tags = $state<Tag[]>([]);
  let composing = $state(false);
  // Two Enter presses in one frame both read `busy === false`, because the
  // prop has not been re-rendered yet; that is how one line became two rows.
  // A plain variable is decided the moment it is set.
  let submitting = false;
  // The draft a result belongs to: a slow parse of "明" must not label "明天".
  let wanted = '';

  async function refresh(text: string) {
    wanted = text;
    if (!isMyGo() || text.trim() === '') {
      tags = [];
      return;
    }
    try {
      const found = await Panel.tags(text);
      if (wanted === text) tags = found;
    } catch {
      if (wanted === text) tags = [];
    }
  }

  function removeTag(tag: Tag) {
    draft = cutFragment(draft, tag.text);
    void refresh(draft);
  }

  async function submit() {
    const text = draft.trim();
    if (!text || busy || submitting) return;
    submitting = true;
    try {
      await onsubmit?.(text);
    } catch {
      // The caller shows the error. Keeping the draft is the point: a failed
      // write used to wipe the line the person had just typed.
      return;
    } finally {
      submitting = false;
    }
    draft = '';
    tags = [];
    wanted = '';
  }

  function onkeydown(event: KeyboardEvent) {
    // keyCode 229 is what Safari and some IMEs report instead of setting
    // isComposing; without it Enter chose a candidate and submitted at once.
    if (event.key === 'Enter' && !event.isComposing && !composing && event.keyCode !== 229) {
      event.preventDefault();
      void submit();
    }
  }
</script>

<div class="quick">
  <input
    type="text"
    bind:value={draft}
    {placeholder}
    aria-label="快速添加"
    autocomplete="off"
    spellcheck="false"
    oninput={() => void refresh(draft)}
    {onkeydown}
    oncompositionstart={() => (composing = true)}
    oncompositionend={() => {
      composing = false;
      void refresh(draft);
    }}
    disabled={busy}
  />
  {#if tags.length > 0}
    <div class="tags" aria-label="识别到的时间">
      {#each tags as tag (tag.kind + tag.text)}
        <span class="chip {tag.kind}">
          {tag.label}
          <button type="button" aria-label="去掉 {tag.label}" onclick={() => removeTag(tag)}>
            <svg viewBox="0 0 10 10" aria-hidden="true"><path d="M2 2 8 8M8 2 2 8" /></svg>
          </button>
        </span>
      {/each}
    </div>
  {/if}
</div>

<style>
  .quick {
    display: flex;
    flex-direction: column;
    gap: 6px;
    /* The field sits in the same content column as the rows above it, so its
       left edge lands on the checkbox column instead of the card's border. */
    padding: 0 12px 0 14px;
  }
  input {
    width: 100%;
    height: 30px;
    padding: 0 10px;
    border-radius: 8px;
    border: 1px solid var(--line);
    background: var(--card);
    color: var(--fg);
    font: inherit;
    box-shadow: 0 1px 1px rgb(20 20 30 / 4%);
  }
  input::placeholder {
    color: var(--faint);
  }
  input:focus {
    outline: none;
    border-color: var(--accent);
    box-shadow: 0 0 0 3px var(--accent-soft);
  }
  .tags {
    display: flex;
    flex-wrap: wrap;
    gap: 4px;
    /* The chips line up with the input's own text, which starts one border and
       ten padding past the field's edge. */
    padding-left: 11px;
  }
  .chip {
    display: inline-flex;
    align-items: center;
    gap: 3px;
    height: 20px;
    padding: 0 3px 0 7px;
    border-radius: 999px;
    background: var(--accent-soft);
    color: var(--accent);
    font-size: 11.5px;
    font-weight: 500;
  }
  .chip.repeat {
    background: var(--pill);
    color: var(--fg-2);
  }
  .chip button {
    display: grid;
    place-items: center;
    width: 15px;
    height: 15px;
    padding: 0;
    border: 0;
    border-radius: 999px;
    background: transparent;
    color: inherit;
    cursor: pointer;
    opacity: 0.7;
  }
  .chip button:hover {
    opacity: 1;
    background: rgb(0 0 0 / 8%);
  }
  .chip svg {
    width: 8px;
    height: 8px;
    stroke: currentcolor;
    stroke-width: 1.6;
    stroke-linecap: round;
    fill: none;
  }
</style>
