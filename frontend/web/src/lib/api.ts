/**
 * The one place the page talks to /api/v2.
 *
 * Every call sends the session cookie (same origin), and every write adds
 * nothing else: the server's checkWriteOrigin accepts a same-origin request
 * from a browser, which is exactly what this is.
 */

import type {
  AccountPayload,
  ApiError,
  DayPayload,
  Device,
  Group,
  HistoryPayload,
  Item,
  QuickAddResult,
  TodayPayload,
  WeatherPayload,
} from '@today/shared';

export class ApiRequestError extends Error {
  constructor(
    readonly status: number,
    message: string,
  ) {
    super(message);
    this.name = 'ApiRequestError';
  }
}

async function call<T>(method: string, path: string, body?: unknown): Promise<T> {
  const res = await fetch(path, {
    method,
    credentials: 'same-origin',
    headers: body === undefined ? {} : { 'Content-Type': 'application/json' },
    body: body === undefined ? undefined : JSON.stringify(body),
  }).catch(() => {
    throw new ApiRequestError(0, '连接失败，请检查网络或确认 Today 正在运行后重试。');
  });

  // A session that expired answers 401 from anywhere — the API, the page, a
  // redirect. Sending the person to the login page is the only useful move,
  // and it has to happen for every 401, not just the ones that come back as
  // HTML: a plain-text 401 used to fall through to JSON.parse below and show
  // a parse error instead of the form.
  if (res.status === 401) {
    window.location.href = '/login';
    throw new ApiRequestError(401, '登录已过期');
  }

  const text = await res.text();
  const isJSON = res.headers.get('Content-Type')?.includes('application/json') ?? false;
  let data: unknown;
  if (text && isJSON) {
    data = JSON.parse(text) as unknown;
  }
  if (!res.ok) {
    const message = (data as ApiError | undefined)?.error ?? `请求失败（${res.status}）`;
    throw new ApiRequestError(res.status, message);
  }
  return data as T;
}

export interface ItemDraft {
  category_id?: string;
  title?: string;
  description?: string;
  date?: string;
  time?: string;
  rrule?: string;
  raw?: string;
  parsed?: boolean;
}

/**
 * What the server hands back when a write can be undone: the method, the path
 * and (when the undo is another PATCH) the body. Pages replay it verbatim.
 */
export interface UndoPath {
  method?: string;
  path: string;
  body?: unknown;
}

export interface ItemPatchBody {
  category_id?: string;
  title?: string;
  description?: string;
  status?: 'open' | 'done' | 'cancelled';
  pinned?: boolean;
  all_day?: boolean;
  rrule?: string;
  date?: string;
  time?: string;
}

export const api = {
  categories: () => call<{ id: string; name: string }[]>('GET', '/api/v2/categories'),
  saveCategory: (id: string, name: string) =>
    call<{ id: string; name: string }>(
      id ? 'PATCH' : 'POST',
      id ? `/api/v2/categories/${encodeURIComponent(id)}` : '/api/v2/categories',
      { name },
    ),
  deleteCategory: (id: string) => call('DELETE', `/api/v2/categories/${encodeURIComponent(id)}`),
  trash: () => call<Item[]>('GET', '/api/v2/trash'),
  deleteForever: (id: string) => call('DELETE', `/api/v2/trash/${encodeURIComponent(id)}`),
  day: () => call<DayPayload>('GET', '/api/v2/day'),
  today: () => call<TodayPayload>('GET', '/api/v2/today'),
  items: (view: 'today' | 'all' = 'today') =>
    call<{ items: Item[] | null }>('GET', `/api/v2/items?view=${view}`),
  item: (id: string) => call<Item>('GET', `/api/v2/items/${encodeURIComponent(id)}`),
  create: (draft: ItemDraft) => call<Item>('POST', '/api/v2/items', draft),
  patch: (id: string, body: ItemPatchBody) =>
    call<{ item: Item; undo?: UndoPath }>('PATCH', `/api/v2/items/${encodeURIComponent(id)}`, body),
  remove: (id: string) =>
    call<{ undo: string }>('DELETE', `/api/v2/items/${encodeURIComponent(id)}`),
  restore: (id: string) => call<Item>('POST', `/api/v2/items/${encodeURIComponent(id)}/restore`),
  setStatus: (id: string, status: 'open' | 'done' | 'cancelled') =>
    call<Item>('POST', `/api/v2/items/${encodeURIComponent(id)}/status`, { status }),
  carry: (ids: string[] | 'all') =>
    call<{
      moved: number;
      skipped: Record<string, string>;
      undo?: { method: string; path: string };
    }>('POST', '/api/v2/items/carry', {
      ids: ids === 'all' ? [] : ids,
      all: ids === 'all',
    }),
  uncarry: (ids: string[]) => call<{ moved: number }>('POST', '/api/v2/items/uncarry', { ids }),
  reorder: (ids: string[]) => call<{ result: string }>('POST', '/api/v2/items/reorder', { ids }),
  parse: (raw: string) => call<QuickAddResult>('POST', '/api/v2/parse', { raw }),
  upcoming: () => call<{ groups: Group[] | null }>('GET', '/api/v2/upcoming'),
  history: () => call<HistoryPayload>('GET', '/api/v2/history'),
  search: (q: string) =>
    call<{ groups: Group[] | null; query: string }>(
      'GET',
      `/api/v2/search?q=${encodeURIComponent(q)}`,
    ),
  devices: () => call<{ devices: Device[] | null }>('GET', '/api/v2/devices'),
  refreshDevice: (id: string) =>
    call<{ result: string }>('POST', `/api/v2/devices/${encodeURIComponent(id)}/refresh`),
  previewSound: (id: string) =>
    call<{ result: string }>('POST', `/api/v2/devices/${encodeURIComponent(id)}/preview-sound`),
  settings: () => call<SettingsPayload>('GET', '/api/v2/settings'),
  updateSettings: (diff: Record<string, string>) =>
    call<{ settings: Record<string, string> }>('PATCH', '/api/v2/settings', diff),
  weather: () => call<WeatherPayload>('GET', '/api/v2/weather'),
  account: () => call<AccountPayload>('GET', '/api/v2/account'),
  setPassword: (current: string, next: string) =>
    call<{ result: string }>('POST', '/api/v2/account/password', { current, next }),
  logoutAll: () => call<{ result: string }>('POST', '/api/v2/account/logout-all'),
  setTheme: (theme: 'system' | 'light' | 'dark') =>
    call<{ theme: string }>('PUT', '/api/v2/account/theme', { theme }),
  /** Undo runs the path the server handed back, so the page never guesses. */
  undo: (undo: UndoPath | string) =>
    typeof undo === 'string'
      ? call<unknown>('POST', undo)
      : call<unknown>(undo.method ?? 'POST', undo.path, undo.body),
};

export interface SettingsPayload {
  settings: Record<string, string>;
  zones: string[] | null;
  proxy_summary: string;
  weather: {
    key_set: boolean;
    masked: string;
    detail: string;
    city: string;
    fetched: string;
    source: string;
    disabled: boolean;
  };
  data_note: string;
}
