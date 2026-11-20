<script lang="ts">
  // The snail mark. Three parts move independently, exactly as SKILL 8.5 asks:
  // the antenna (appendage) swings first, the tail tip follows, the body leans
  // last. It plays once when the window appears and once per hover; people who
  // ask for less motion get the still mark.
  import { snail } from '@today/shared/snail';

  let { size = 22, play = true }: { size?: number; play?: boolean } = $props();

  let turns = $state(0);
  let reduced = $state(false);
  let played = $state(false);

  $effect(() => {
    const mq = window.matchMedia('(prefers-reduced-motion: reduce)');
    reduced = mq.matches;
    const onChange = (e: MediaQueryListEvent) => (reduced = e.matches);
    mq.addEventListener('change', onChange);
    return () => mq.removeEventListener('change', onChange);
  });

  // One key change per trigger: re-mounting the animations restarts them
  // without resetting the SVG geometry.
  const restart = () => {
    if (reduced) return;
    turns += 1;
  };

  $effect(() => {
    if (play && !played && !reduced) {
      played = true;
      restart();
    }
  });

  function onEnter() {
    restart();
  }
</script>

<svg
  viewBox="0 0 {snail.size} {snail.size}"
  width={size}
  height={size}
  role="img"
  aria-label="Today"
  onmouseenter={onEnter}
  class:moving={!reduced}
>
  {#key turns}
    <!-- The shell and the foot stay put; only the antenna and the tail move. -->
    <g class="body">
      <path d={snail.body} fill="currentColor" fill-rule="nonzero" />
    </g>
    <g
      class="antenna"
      style="transform-origin: {snail.antennaPivot[0]}px {snail.antennaPivot[1]}px"
    >
      <path d={snail.antenna} fill="currentColor" fill-rule="nonzero" />
    </g>
    <g class="tail" style="transform-origin: {snail.tailPivot[0]}px {snail.tailPivot[1]}px">
      <path d={snail.tail} fill="currentColor" fill-rule="nonzero" />
    </g>
  {/key}
</svg>

<style>
  svg {
    display: block;
    overflow: visible;
    color: var(--fg);
  }
  /* Degrees follow SKILL 8.5. The antenna leads, the tail answers, the body
     leans last and least; all three settle on the still drawing. */
  .moving :global(.antenna) {
    animation: antenna 0.9s cubic-bezier(0.3, 0, 0.3, 1) both;
  }
  .moving :global(.tail) {
    animation: tail 0.9s cubic-bezier(0.3, 0, 0.3, 1) both;
  }
  .moving :global(.body) {
    animation: body 0.9s cubic-bezier(0.3, 0, 0.3, 1) both;
  }
  @keyframes antenna {
    0% {
      transform: rotate(10deg);
    }
    25% {
      transform: rotate(-3deg);
    }
    50% {
      transform: rotate(8deg);
    }
    70% {
      transform: rotate(-2deg);
    }
    85% {
      transform: rotate(3deg);
    }
    100% {
      transform: rotate(0deg);
    }
  }
  @keyframes tail {
    0% {
      transform: rotate(0deg);
    }
    20% {
      transform: rotate(16deg);
    }
    45% {
      transform: rotate(-7deg);
    }
    65% {
      transform: rotate(9deg);
    }
    82% {
      transform: rotate(-3deg);
    }
    92% {
      transform: rotate(2deg);
    }
    100% {
      transform: rotate(0deg);
    }
  }
  @keyframes body {
    0% {
      transform: rotate(0deg);
    }
    30% {
      transform: rotate(-4deg);
    }
    60% {
      transform: rotate(2deg);
    }
    85% {
      transform: rotate(-1deg);
    }
    100% {
      transform: rotate(0deg);
    }
  }
  .body {
    transform-origin: 16px 22px;
  }
  @media (prefers-reduced-motion: reduce) {
    .moving :global(.antenna),
    .moving :global(.tail),
    .moving :global(.body) {
      animation: none;
    }
  }
</style>
