import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from './ui/select';
import { Popover, PopoverContent, PopoverTrigger } from './ui/popover';
import { Button } from './ui/button';
import { Clock } from 'lucide-react';

export function TimePicker({
  value,
  onChange,
}: {
  value: string;
  onChange: (value: string) => void;
}) {
  const [hour, minute] = (value || '09:00').split(':');
  return (
    <Popover>
      <PopoverTrigger asChild>
        <Button variant="outline" size="sm" aria-label="选择时间">
          <Clock className="size-3.5" />
          {value || '全天'}
        </Button>
      </PopoverTrigger>
      <PopoverContent className="w-[220px] space-y-3" align="start">
        <div className="flex items-center gap-2">
          <Select value={hour} onValueChange={(v) => onChange(`${v}:${minute}`)}>
            <SelectTrigger aria-label="小时">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {Array.from({ length: 24 }, (_, i) => String(i).padStart(2, '0')).map((v) => (
                <SelectItem key={v} value={v}>
                  {v} 时
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
          <span>:</span>
          <Select value={minute} onValueChange={(v) => onChange(`${hour}:${v}`)}>
            <SelectTrigger aria-label="分钟">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {Array.from({ length: 60 }, (_, i) => String(i).padStart(2, '0')).map((v) => (
                <SelectItem key={v} value={v}>
                  {v} 分
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>
        <Button
          type="button"
          variant="ghost"
          size="sm"
          className="w-full"
          onClick={() => onChange('')}
        >
          设为全天
        </Button>
      </PopoverContent>
    </Popover>
  );
}
