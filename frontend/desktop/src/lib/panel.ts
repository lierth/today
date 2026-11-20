/**
 * The panel's writes go through here, one item at a time, so a fast double
 * click cannot interleave. The rule mirrors the web page: flip the row now,
 * and if the write fails, reload the truth and say so.
 */
import { Panel, type TodayView, type Item, type Carried } from '../mygo';
import { isMyGo } from 'mygo-runtime';

/** optimistic calls the server and rolls the local row back on failure. */
export async function toggleDone(view: TodayView, id: string, done: boolean): Promise<TodayView> {
  const row = view.items.find((it) => it.id === id);
  const before = row ? row.done : !done;
  if (row) row.done = done;
  try {
    await Panel.setDone(id, done);
    return await Panel.today();
  } catch (err) {
    if (row) row.done = before;
    throw err;
  }
}

/** carryToday moves every leftover to today, or just the one named. */
export async function carryToday(id: string): Promise<TodayView> {
  await Panel.carry(id ? [id] : [], !id);
  return await Panel.today();
}

/** cutFragment removes one chip's text from the draft and tidies the spaces
 * around it. The chip stands for an exact slice of what the person typed, so
 * cutting that slice is the only way to undo a parse without re-parsing. */
export function cutFragment(draft: string, text: string): string {
  const at = draft.indexOf(text);
  if (at < 0) return draft;
  return (draft.slice(0, at) + draft.slice(at + text.length)).replace(/\s+/g, ' ').trim();
}

/** carriedDay is the "之前" hint under a row: the day it came from. */
export function carriedDay(carried: Carried[], id: string): string {
  return carried.find((c) => c.id === id)?.day ?? '';
}

/** statusOf turns a store row into the class the checkbox draws. */
export function statusClass(item: Item): string {
  return item.done ? 'row done' : 'row';
}

export const live = isMyGo();
