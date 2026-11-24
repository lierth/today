import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { toast } from 'sonner';
import { api, type UndoPath } from './api';
import type {
  AccountPayload,
  DayPayload,
  Device,
  Group,
  HistoryPayload,
  Item,
  QuickAddResult,
  TodayPayload,
  WeatherPayload,
} from '@today/shared';

/** One place for the keys, so an invalidation never misses a screen. */
export const keys = {
  today: ['today'] as const,
  day: ['day'] as const,
  items: ['items'] as const,
  upcoming: ['upcoming'] as const,
  history: ['history'] as const,
  search: (q: string) => ['search', q] as const,
  devices: ['devices'] as const,
  account: ['account'] as const,
  weather: ['weather'] as const,
  item: (id: string) => ['item', id] as const,
};

export function useToday() {
  return useQuery({ queryKey: keys.today, queryFn: () => api.today() });
}

export function useUpcoming() {
  return useQuery({ queryKey: keys.upcoming, queryFn: () => api.upcoming() });
}

export function useHistory() {
  return useQuery({ queryKey: keys.history, queryFn: () => api.history() });
}

export function useSearch(query: string) {
  return useQuery({
    queryKey: keys.search(query),
    queryFn: () => api.search(query),
    enabled: query.trim().length > 0,
  });
}

export function useDevices() {
  return useQuery({ queryKey: keys.devices, queryFn: () => api.devices() });
}

export function useAccount() {
  return useQuery({ queryKey: keys.account, queryFn: () => api.account() });
}

export function useWeather() {
  return useQuery({ queryKey: keys.weather, queryFn: () => api.weather() });
}

export type {
  AccountPayload,
  DayPayload,
  Device,
  Group,
  HistoryPayload,
  Item,
  QuickAddResult,
  TodayPayload,
  WeatherPayload,
};

/**
 * Everything a write can change: the day's list, the counts, the upcoming
 * buckets and the history are all derived from the same rows.
 */
export function useRefreshLists() {
  const client = useQueryClient();
  return async () => {
    await Promise.all([
      client.invalidateQueries({ queryKey: keys.today }),
      client.invalidateQueries({ queryKey: keys.day }),
      client.invalidateQueries({ queryKey: keys.items }),
      client.invalidateQueries({ queryKey: keys.upcoming }),
      client.invalidateQueries({ queryKey: keys.history }),
      // The palette and the search page read their own key; the drawer reads
      // the item by id. Without these a rename showed in the list and stayed
      // old in the two places that are nothing but a view of the same row.
      client.invalidateQueries({ queryKey: ['search'] }),
      client.invalidateQueries({ queryKey: ['item'] }),
    ]);
  };
}

/**
 * The undo is whatever the server handed back — a path, and sometimes a body
 * ("put the pin back to true") — so the toast never has to know what kind of
 * change it is undoing.
 */
export function useUndo() {
  const refresh = useRefreshLists();
  return async (undo: UndoPath | string, message: string) => {
    try {
      await api.undo(undo);
      await refresh();
      toast.success(message);
    } catch (err) {
      toast.error(err instanceof Error ? err.message : String(err));
    }
  };
}

/** A circular tick that flips at once and goes back if the write fails. */
export function useToggleDone() {
  const client = useQueryClient();
  const refresh = useRefreshLists();
  const undo = useUndo();
  const failed = (err: unknown) => toast.error(err instanceof Error ? err.message : String(err));

  return useMutation({
    mutationFn: (vars: { item: Item; done: boolean }) =>
      api.patch(vars.item.id, { status: vars.done ? 'done' : 'open' }),
    onMutate: async ({ item, done }) => {
      await client.cancelQueries({ queryKey: keys.today });
      const before = client.getQueryData<TodayPayload>(keys.today);
      if (before) {
        client.setQueryData<TodayPayload>(keys.today, {
          ...before,
          items: (before.items ?? []).map((row) =>
            row.id === item.id ? { ...row, status: done ? 'done' : 'open' } : row,
          ),
        });
      }
      return { before };
    },
    onError: (err, _vars, context) => {
      if (context?.before) client.setQueryData(keys.today, context.before);
      failed(err);
    },
    onSuccess: async (out, vars) => {
      await refresh();
      if (out.undo) {
        toast(vars.done ? '已完成' : '又放回未完成', {
          action: {
            label: '撤销',
            onClick: () => void undo(out.undo!, vars.done ? '已改回未完成' : '已改回完成'),
          },
        });
      }
    },
  });
}

export function useDeleteItem() {
  const refresh = useRefreshLists();
  const undo = useUndo();
  return useMutation({
    mutationFn: (id: string) => api.remove(id),
    onSuccess: async (out) => {
      await refresh();
      toast('已删除', {
        description: '还可以撤销。',
        action: { label: '撤销', onClick: () => void undo(out.undo, '已恢复') },
      });
    },
    onError: (err) => toast.error(err instanceof Error ? err.message : String(err)),
  });
}

export function useCarry() {
  const refresh = useRefreshLists();
  const undo = useUndo();
  return useMutation({
    mutationFn: (vars: { ids: string[]; all?: boolean }) => api.carry(vars.all ? 'all' : vars.ids),
    onSuccess: async (out) => {
      await refresh();
      if (out.moved > 0) {
        const undoPath = out.undo?.path;
        toast(`已移到今天：${out.moved} 项`, {
          action: undoPath
            ? { label: '撤销', onClick: () => void undo(undoPath, '已放回') }
            : undefined,
        });
      }
    },
    onError: (err) => toast.error(err instanceof Error ? err.message : String(err)),
  });
}

export function usePatchItem() {
  const refresh = useRefreshLists();
  const undo = useUndo();
  return useMutation({
    mutationFn: (vars: { id: string; patch: Parameters<typeof api.patch>[1] }) =>
      api.patch(vars.id, vars.patch),
    onSuccess: async (out, vars) => {
      await refresh();
      if (!out.undo) return;
      // A pin or a move: the server sent back the fields that changed.
      const what =
        vars.patch.pinned !== undefined
          ? vars.patch.pinned
            ? '已置顶'
            : '已取消置顶'
          : '已改到明天';
      toast(what, {
        action: { label: '撤销', onClick: () => void undo(out.undo!, `${what} · 已撤销`) },
      });
    },
    onError: (err) => toast.error(err instanceof Error ? err.message : String(err)),
  });
}

/** Drag-reorder: the list is optimistic, the write follows in the new order. */
export function useReorder() {
  const client = useQueryClient();
  return useMutation({
    mutationFn: (ids: string[]) => api.reorder(ids),
    onMutate: async (ids) => {
      await client.cancelQueries({ queryKey: keys.today });
      const before = client.getQueryData<TodayPayload>(keys.today);
      if (before) {
        const rank = new Map(ids.map((id, index) => [id, index]));
        const rows = [...(before.items ?? [])].sort(
          (a, b) => (rank.get(a.id) ?? a.sort_key) - (rank.get(b.id) ?? b.sort_key),
        );
        client.setQueryData<TodayPayload>(keys.today, { ...before, items: rows });
      }
      return { before };
    },
    onError: (err, _ids, context) => {
      if (context?.before) client.setQueryData(keys.today, context.before);
      toast.error(err instanceof Error ? err.message : String(err));
    },
    onSettled: () => void client.invalidateQueries({ queryKey: keys.today }),
  });
}
