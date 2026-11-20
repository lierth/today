import { clsx, type ClassValue } from 'clsx';
import { twMerge } from 'tailwind-merge';

/** cn merges conditional classes and lets a later Tailwind class win. */
export function cn(...inputs: ClassValue[]) {
  return twMerge(clsx(inputs));
}
