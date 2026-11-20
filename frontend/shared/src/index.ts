/**
 * The shapes /api/v2 answers with.
 *
 * These mirror the JSON tags of internal/store and internal/server, so a
 * change on the Go side that the page would miss is a compile error here.
 */

export type ItemStatus = 'open' | 'done' | 'cancelled';

export interface Item {
  id: string;
  title: string;
  description?: string;
  all_day: boolean;
  starts_at?: string;
  due_at?: string;
  date_local: string;
  status: ItemStatus;
  pinned: boolean;
  rrule?: string;
  series_id?: string;
  sort_key: number;
  carried_from?: string;
  manual_rank?: number;
  created_at: string;
  updated_at: string;
}

export interface Day {
  key: string;
  label: string;
  weekday: string;
  lunar: string;
  lunar_tag?: string;
}

export interface Group {
  Label: string;
  Items: Item[] | null;
}

export interface CarriedItem extends Item {
  day_label: string;
}

export interface TodayPayload {
  day: Day;
  items: Item[] | null;
  total: number;
  done: number;
  carried: CarriedItem[] | null;
  auto_carry: boolean;
  board_limit: number;
  weather: string;
  preview_id: string;
}

export interface DayPayload extends Day {
  lunar_tag?: string;
  progress: string;
  total: number;
  done: number;
  weather: string;
}

export type TagKind = 'date' | 'time' | 'repeat';

export interface QuickAddTag {
  kind: TagKind;
  label: string;
  /** The slice of the draft the chip stands for; the × deletes exactly this. */
  text: string;
}

export interface QuickAddResult {
  title: string;
  date: string;
  time: string;
  rrule: string;
  tags: QuickAddTag[] | null;
}

export interface Device {
  id: string;
  name: string;
  panel_w: number;
  panel_h: number;
  row_h: number;
  font_size: number;
  battery_mv?: number;
  temp_c?: number;
  humidity?: number;
  last_seen_at?: string;
  created_at: string;
  // Derived server-side so the page and the tray agree on the wording.
  online: boolean;
  battery_label: string;
  climate: string;
  since: string;
  firmware: string;
  city: string;
  preview: boolean;
}

export interface HistoryPayload {
  groups: Group[] | null;
  past: Item[] | null;
  done_this_week: number;
  done_last_week: number;
}

export interface WeatherPayload {
  line: string;
  city: string;
  key_set: boolean;
}

export interface AccountPayload {
  theme: 'system' | 'light' | 'dark';
  has_password: boolean;
}

export interface ApiError {
  error: string;
}

/** One chip the strip shows: a recognised tag the person may delete. */
export interface Chip {
  kind: TagKind;
  label: string;
  text: string;
}
