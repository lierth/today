import { useState } from 'react';
import { KeyRound, LogOut } from 'lucide-react';
import { toast } from 'sonner';
import { api } from '@/lib/api';
import { Panel } from '@/components/ui/panel';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { useTheme, type ThemeChoice } from '@/lib/theme';

// The web settings page is deliberately thin: the Mac app owns devices,
// backups, firmware and the tray. What a browser needs is the account — the
// password, the session, and the appearance — so that is all this page is.
export default function SettingsPage() {
  const { theme, save } = useTheme();
  const [current, setCurrent] = useState('');
  const [next, setNext] = useState('');
  const [again, setAgain] = useState('');
  const [busy, setBusy] = useState(false);

  async function changePassword(event: React.FormEvent) {
    event.preventDefault();
    if (next !== again) {
      toast.error('两次输入的新口令不一样');
      return;
    }
    setBusy(true);
    try {
      await api.setPassword(current, next);
      setCurrent('');
      setNext('');
      setAgain('');
      toast.success('口令已改，其它设备需要重新登录');
    } catch (err) {
      toast.error((err as Error).message);
    } finally {
      setBusy(false);
    }
  }

  async function logoutAll() {
    if (!window.confirm('所有设备（包括这台）都要重新登录。继续？')) return;
    try {
      await api.logoutAll();
      window.location.href = '/login';
    } catch (err) {
      toast.error((err as Error).message);
    }
  }

  // useTheme is a shared store now, so the sidebar's control and this one
  // always agree; nothing to sync here.

  const themes: { value: ThemeChoice; label: string }[] = [
    { value: 'system', label: '跟随系统' },
    { value: 'light', label: '浅色' },
    { value: 'dark', label: '深色' },
  ];

  return (
    <div className="space-y-6">
      <h1 className="text-[20px] font-[600]">设置</h1>

      <Panel className="p-4">
        <h2 className="mb-3 text-[11.5px] font-[560] text-[var(--muted)]">外观</h2>
        {/* A segmented control: one track, one white slider on the chosen
            segment. Three separate outlined buttons read as three actions. */}
        <div
          className="inline-flex rounded-[9px] bg-[var(--pill)] p-0.5"
          role="radiogroup"
          aria-label="主题"
        >
          {themes.map((item) => (
            <button
              key={item.value}
              type="button"
              role="radio"
              aria-checked={theme === item.value}
              onClick={() => void save(item.value)}
              className={
                theme === item.value
                  ? 'h-7 rounded-[7px] bg-[var(--card)] px-3 text-[12px] font-[560] shadow-sm'
                  : 'h-7 rounded-[7px] px-3 text-[12px] text-[var(--ctl-fg)] hover:text-[var(--fg)]'
              }
            >
              {item.label}
            </button>
          ))}
        </div>
      </Panel>

      <Panel className="p-4">
        <h2 className="mb-3 flex items-center gap-2 text-[11.5px] font-[560] text-[var(--muted)]">
          <KeyRound size={13} aria-hidden /> 安全
        </h2>
        <form className="space-y-3" onSubmit={(e) => void changePassword(e)}>
          <label className="block text-[12px]">
            <span className="mb-1 block text-[var(--fg-2)]">现在的口令</span>
            <Input
              type="password"
              value={current}
              autoComplete="current-password"
              onChange={(e) => setCurrent(e.target.value)}
              required
            />
          </label>
          <label className="block text-[12px]">
            <span className="mb-1 block text-[var(--fg-2)]">新口令（至少 8 位）</span>
            <Input
              type="password"
              value={next}
              autoComplete="new-password"
              minLength={8}
              onChange={(e) => setNext(e.target.value)}
              required
            />
          </label>
          <label className="block text-[12px]">
            <span className="mb-1 block text-[var(--fg-2)]">再输一次</span>
            <Input
              type="password"
              value={again}
              autoComplete="new-password"
              onChange={(e) => setAgain(e.target.value)}
              required
            />
          </label>
          <div className="flex items-center justify-between pt-1">
            <Button type="submit" size="sm" disabled={busy}>
              改口令
            </Button>
            <Button type="button" variant="ghost" size="sm" onClick={() => void logoutAll()}>
              <LogOut size={13} className="mr-1" aria-hidden /> 登出所有设备
            </Button>
          </div>
        </form>
      </Panel>

      <p className="text-[12px] text-[var(--muted)]">
        设备、备份、固件和托盘设置都在 Mac app 的设置窗口里：菜单栏图标右键 →「设置…」。
      </p>
    </div>
  );
}
