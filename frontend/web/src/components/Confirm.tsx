import { createContext, useContext, useState, useRef } from 'react';
import type { ReactNode } from 'react';
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
  DialogFooter,
} from './ui/dialog';
import { Button } from './ui/button';

type Ask = (title: string, description: string, destructive?: boolean) => Promise<boolean>;
const Context = createContext<Ask>(async () => false);
export const useConfirm = () => useContext(Context);
export class ActionCancelled extends Error {}

export function ConfirmProvider({ children }: { children: ReactNode }) {
  const [question, setQuestion] = useState<{
    title: string;
    description: string;
    destructive: boolean;
  } | null>(null);
  const answer = useRef<((value: boolean) => void) | null>(null);
  const finish = (value: boolean) => {
    answer.current?.(value);
    answer.current = null;
    setQuestion(null);
  };
  const ask: Ask = (title, description, destructive = true) =>
    new Promise((resolve) => {
      answer.current?.(false);
      answer.current = resolve;
      setQuestion({ title, description, destructive });
    });
  return (
    <Context.Provider value={ask}>
      {children}
      <Dialog
        open={Boolean(question)}
        onOpenChange={(open) => {
          if (!open) finish(false);
        }}
      >
        <DialogContent className="max-w-[360px] gap-4" aria-describedby="confirm-description">
          <DialogHeader>
            <DialogTitle>{question?.title}</DialogTitle>
            <DialogDescription id="confirm-description">{question?.description}</DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button variant="secondary" autoFocus onClick={() => finish(false)}>
              取消
            </Button>
            <Button
              variant={question?.destructive ? 'destructive' : 'default'}
              onClick={() => finish(true)}
            >
              确认
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </Context.Provider>
  );
}
