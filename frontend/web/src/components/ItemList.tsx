import { useMemo } from 'react';
import {
  DndContext,
  KeyboardSensor,
  PointerSensor,
  closestCenter,
  useSensor,
  useSensors,
  type DragEndEvent,
} from '@dnd-kit/core';
import {
  SortableContext,
  arrayMove,
  sortableKeyboardCoordinates,
  useSortable,
  verticalListSortingStrategy,
} from '@dnd-kit/sortable';
import { CSS } from '@dnd-kit/utilities';
import { GripVertical } from 'lucide-react';
import { cn } from '@/lib/utils';
import { Hint } from './Hint';
import { ItemRow } from './ItemRow';
import type { Item } from '@today/shared';

/**
 * The day's rows, in one draggable list.
 *
 * The board shows the first few rows and the page draws the rest under a
 * labelled rule — but it is one list. Dragging a row across the rule is how a
 * person says "put this on the board", so the drag spans both halves and the
 * ids handed to the server are the whole list's, not the visible half's.
 */
export function ItemList<T extends Item>({
  items,
  boardCount,
  divider,
  render,
  onReorder,
}: {
  /** Every row of the day, in display order. */
  items: T[];
  /** How many of them the board draws before the rule. */
  boardCount: number;
  /** The rule itself, drawn between the two halves. */
  divider?: React.ReactNode;
  render: (item: T) => Omit<Parameters<typeof ItemRow>[0], 'dragHandle' | 'dimmed'>;
  onReorder: (ids: string[]) => void;
}) {
  const sensors = useSensors(
    useSensor(PointerSensor, { activationConstraint: { distance: 4 } }),
    useSensor(KeyboardSensor, { coordinateGetter: sortableKeyboardCoordinates }),
  );
  const ids = useMemo(() => items.map((item) => item.id), [items]);

  const onDragEnd = (event: DragEndEvent) => {
    const { active, over } = event;
    if (!over || active.id === over.id) return;
    const from = ids.indexOf(String(active.id));
    const to = ids.indexOf(String(over.id));
    if (from < 0 || to < 0) return;
    onReorder(arrayMove(ids, from, to));
  };

  return (
    <DndContext sensors={sensors} collisionDetection={closestCenter} onDragEnd={onDragEnd}>
      <SortableContext items={ids} strategy={verticalListSortingStrategy}>
        {items.map((item, index) => (
          <div key={item.id} className="contents">
            {index === boardCount && divider ? divider : null}
            <SortableRow id={item.id} dimmed={index >= boardCount} render={() => render(item)} />
          </div>
        ))}
      </SortableContext>
    </DndContext>
  );
}

function SortableRow({
  id,
  dimmed,
  render,
}: {
  id: string;
  dimmed: boolean;
  render: () => Omit<Parameters<typeof ItemRow>[0], 'dragHandle' | 'dimmed'>;
}) {
  const { attributes, listeners, setNodeRef, transform, transition, isDragging } = useSortable({
    id,
  });
  return (
    <div
      ref={setNodeRef}
      style={{ transform: CSS.Translate.toString(transform), transition }}
      className={cn(isDragging && 'relative z-10 bg-[var(--card)]')}
    >
      <ItemRow
        {...render()}
        dimmed={dimmed}
        dragHandle={
          <Hint label="拖动排序">
            <button
              type="button"
              aria-label="拖动排序"
              className={cn(
                'grid size-[18px] shrink-0 cursor-grab place-items-center rounded-[5px]',
                'text-[var(--faint)] hover:text-[var(--fg)] active:cursor-grabbing',
                // Hidden until the row is hovered or the grip itself is focused:
                // it takes no layout width the text could have used, and a
                // keyboard user still sees it.
                'opacity-0 group-hover:opacity-100 focus-visible:opacity-100',
                'focus-visible:outline-2 focus-visible:outline-offset-1 focus-visible:outline-[var(--accent)]',
              )}
              {...attributes}
              {...listeners}
            >
              <GripVertical className="size-3.5" />
            </button>
          </Hint>
        }
      />
    </div>
  );
}
