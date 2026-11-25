import { lazy, Suspense, useState, useEffect } from 'react';
import { NavLink, Outlet } from 'react-router-dom';
import {
  CalendarDays,
  History,
  LayoutList,
  LogOut,
  Moon,
  Plus,
  Search,
  Sun,
  SunMoon,
  Settings2,
  ChevronUp,
  Tags,
  Trash2,
  KeyRound,
} from 'lucide-react';
import { TooltipProvider } from './components/Hint';
import { Snail } from './components/Snail';
import { useTheme } from './lib/theme';
import { api } from './lib/api';

import { cn } from './lib/utils';
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu';
import { Button } from '@/components/ui/button';
import { CreateItemDialog } from './components/CreateItemDialog';

// The palette is "open something": it is not on screen when the page paints,
// and it carries cmdk and Radix's dialog. It ships after the first paint.
const CommandPalette = lazy(() =>
  import('./components/CommandPalette').then((m) => ({ default: m.CommandPalette })),
);

const NAV = [
  { to: '/', label: '今日', icon: LayoutList, end: true },
  { to: '/upcoming', label: '即将到来', icon: CalendarDays, end: false },
  { to: '/history', label: '历史', icon: History, end: false },
  { to: '/search', label: '搜索', icon: Search, end: false },
  { to: '/categories', label: '分类', icon: Tags, end: false },
  { to: '/trash', label: '回收站', icon: Trash2, end: false },
];

/**
 * The shell: a narrow rail on the desktop, a tab bar at the bottom on a phone.
 * Both are the same four places, so the app has one map, not two.
 */
export function App() {
  const { theme, save } = useTheme();

  return (
    <TooltipProvider delayDuration={400}>
      <div className="flex min-h-dvh bg-[var(--bg)] text-[var(--fg)]">
        <aside className="sticky top-0 hidden h-dvh w-[196px] shrink-0 flex-col border-r border-[var(--line)] px-2.5 py-3 md:flex">
          <div className="mb-4 flex items-center gap-2 px-1.5">
            <Snail size={20} className="text-[var(--fg)]" />
            <span className="text-[13px] font-[560] tracking-tight">today</span>
          </div>
          {/* A visible way into search on the desktop: ⌘K is the fast path,
              not the only one. It is a link, not a button that asks the
              palette to open — a click in the first frames after paint would
              arrive before the palette's chunk has registered its listener. */}
          <NavLink
            to="/search"
            className="mb-3 flex h-7 items-center gap-2 rounded-[7px] border border-[var(--line)] bg-[var(--card)] px-2 text-[12px] text-[var(--muted)] hover:border-[var(--line-2)] hover:text-[var(--fg-2)]"
          >
            <Search className="size-3.5" />
            <span className="flex-1 text-left">搜索</span>
            <kbd className="rounded-[4px] border border-[var(--line)] px-1 text-[10px] text-[var(--faint)]">
              ⌘K
            </kbd>
          </NavLink>
          <nav className="flex flex-col gap-0.5" aria-label="主导航">
            {NAV.map(({ to, label, icon: Icon, end }) => (
              <NavLink
                key={to}
                to={to}
                end={end}
                className={({ isActive }) =>
                  cn(
                    'flex h-8 items-center gap-2.5 rounded-[7px] px-2 text-[12.5px]',
                    isActive
                      ? 'bg-[var(--sel)] font-[560] text-[var(--fg)]'
                      : 'text-[var(--ctl-fg)] hover:bg-[var(--pill)] hover:text-[var(--fg)]',
                  )
                }
              >
                <Icon className="size-4" />
                {label}
              </NavLink>
            ))}
          </nav>
          <div className="mt-auto">
            <AccountMenu theme={theme} onTheme={save} />
          </div>
        </aside>

        <div className="flex min-w-0 flex-1 flex-col">
          {/* Phones have no rail, so the account (theme, password, sign-out)
              and the search door live in a thin bar at the top instead. */}
          <div className="flex items-center gap-1 px-3 pt-2 md:hidden">
            <Snail size={18} className="text-[var(--fg)]" />
            <span className="text-[12.5px] font-[560] tracking-tight">today</span>
            <span className="flex-1" />
            <MobileAccount theme={theme} onTheme={save} />
          </div>
          {/* 760 px keeps a line of text readable; at 1280 and up the rail
              already takes 196 of it, so the column gets a little more room. */}
          <main className="mx-auto w-full max-w-[760px] flex-1 px-4 pt-5 pb-28 md:px-8 md:pt-8 md:pb-10 xl:max-w-[820px]">
            <Outlet />
          </main>
        </div>

        <nav
          className="fixed inset-x-0 bottom-0 z-20 flex border-t border-[var(--line)] bg-[color-mix(in_oklab,var(--card)_92%,transparent)] backdrop-blur md:hidden"
          aria-label="主导航"
        >
          {NAV.slice(0, 4).map(({ to, label, icon: Icon, end }) => (
            <NavLink
              key={to}
              to={to}
              end={end}
              className={({ isActive }) =>
                cn(
                  'flex flex-1 flex-col items-center gap-0.5 py-2 text-[10.5px]',
                  isActive ? 'text-[var(--fg)] font-[560]' : 'text-[var(--muted)]',
                )
              }
            >
              <Icon className="size-[18px]" />
              {label}
            </NavLink>
          ))}
        </nav>

        <MobileAdd />

        {/* Mounted at once, shipped on demand: whichever the person opens
            first pulls its chunk. Nothing visible waits on it. */}
        <Suspense fallback={null}>
          <CommandPalette />
        </Suspense>
      </div>
    </TooltipProvider>
  );
}

/**
 * The account menu for a phone. The rail is hidden there, and the four tabs
 * are full, so theme, password and sign-out would otherwise be unreachable.
 */
function MobileAccount({
  theme,
  onTheme,
}: {
  theme: 'system' | 'light' | 'dark';
  onTheme: (next: 'system' | 'light' | 'dark') => void;
}) {
  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button variant="ghost" size="icon-sm" aria-label="账号与主题" className="size-8">
          <Settings2 className="size-4" />
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" sideOffset={8} className="w-[208px] p-1.5">
        <DropdownMenuRadioGroup
          value={theme}
          onValueChange={(v) => onTheme(v as 'system' | 'light' | 'dark')}
        >
          <DropdownMenuRadioItem value="system">
            <SunMoon className="size-4" /> 跟随系统
          </DropdownMenuRadioItem>
          <DropdownMenuRadioItem value="light">
            <Sun className="size-4" /> 浅色
          </DropdownMenuRadioItem>
          <DropdownMenuRadioItem value="dark">
            <Moon className="size-4" /> 深色
          </DropdownMenuRadioItem>
        </DropdownMenuRadioGroup>
        <DropdownMenuSeparator />
        <DropdownMenuItem asChild>
          <NavLink to="/categories">
            <Tags className="size-4" />
            管理分类
          </NavLink>
        </DropdownMenuItem>
        <DropdownMenuItem asChild>
          <NavLink to="/trash">
            <Trash2 className="size-4" />
            回收站
          </NavLink>
        </DropdownMenuItem>
        <DropdownMenuSeparator />
        <DropdownMenuItem asChild>
          <a href="/settings">设置</a>
        </DropdownMenuItem>
        <DropdownMenuItem asChild>
          <a href="/settings#security">
            <KeyRound className="size-4" />
            修改口令
          </a>
        </DropdownMenuItem>
        <DropdownMenuItem
          onSelect={() => {
            if (!window.confirm('在所有设备上退出登录？口令不变，但每台设备都要重新登录。')) return;
            void api.logoutAll().then(() => {
              window.location.href = '/login';
            });
          }}
        >
          <LogOut className="size-4" /> 登出所有设备
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}

/**
 * The phone's one primary action: a floating + that opens the quick-add field
 * in a sheet over the list. Desktop has the field inline; a phone has no room
 * for it above the fold.
 */
function MobileAdd() {
  const [open, setOpen] = useState(false);
  useEffect(() => {
    const add = () => setOpen(true);
    window.addEventListener('today:add', add);
    if (new URLSearchParams(window.location.search).get('focus') === 'add') setOpen(true);
    return () => window.removeEventListener('today:add', add);
  }, []);
  return (
    <>
      <Button
        className="fixed bottom-20 right-4 z-30 size-11 rounded-full shadow-lg md:hidden"
        size="icon"
        aria-label="添加事项"
        onClick={() => setOpen(true)}
      >
        <Plus className="size-5" />
      </Button>
      <CreateItemDialog open={open} onOpenChange={setOpen} />
    </>
  );
}

function AccountMenu({
  theme,
  onTheme,
}: {
  theme: 'system' | 'light' | 'dark';
  onTheme: (next: 'system' | 'light' | 'dark') => void;
}) {
  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button
          variant="ghost"
          className="h-9 w-full justify-start gap-2.5 rounded-[7px] px-2 text-[12.5px] text-[var(--ctl-fg)]"
          aria-label="账号与主题"
        >
          <Settings2 className="size-4" />
          <span className="flex-1 text-left">设置</span>
          <ChevronUp className="size-3 text-[var(--muted)]" />
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="start" sideOffset={8} className="w-[208px] p-1.5">
        <DropdownMenuRadioGroup
          value={theme}
          onValueChange={(v) => onTheme(v as 'system' | 'light' | 'dark')}
        >
          <DropdownMenuRadioItem value="system">
            <SunMoon className="size-4" /> 跟随系统
          </DropdownMenuRadioItem>
          <DropdownMenuRadioItem value="light">
            <Sun className="size-4" /> 浅色
          </DropdownMenuRadioItem>
          <DropdownMenuRadioItem value="dark">
            <Moon className="size-4" /> 深色
          </DropdownMenuRadioItem>
        </DropdownMenuRadioGroup>
        <DropdownMenuSeparator />
        <DropdownMenuItem asChild>
          <NavLink to="/categories">
            <Tags className="size-4" />
            管理分类
          </NavLink>
        </DropdownMenuItem>
        <DropdownMenuItem asChild>
          <NavLink to="/trash">
            <Trash2 className="size-4" />
            回收站
          </NavLink>
        </DropdownMenuItem>
        <DropdownMenuSeparator />
        <DropdownMenuItem asChild>
          <a href="/settings#security">
            <KeyRound className="size-4" />
            修改口令
          </a>
        </DropdownMenuItem>
        <DropdownMenuItem
          onSelect={() => {
            if (!window.confirm('在所有设备上退出登录？口令不变，但每台设备都要重新登录。')) return;
            void api.logoutAll().then(() => {
              window.location.href = '/login';
            });
          }}
        >
          <LogOut className="size-4" /> 登出所有设备
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
