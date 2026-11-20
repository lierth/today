import { StrictMode, Suspense, lazy } from 'react';
import { createRoot } from 'react-dom/client';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { BrowserRouter, Route, Routes } from 'react-router-dom';
import { App } from './App';
import { Toaster } from './components/ui/sonner';
import { Skeleton } from './components/ui/skeleton';
import './index.css';

// Every screen loads on demand. Upcoming, history and the settings form are
// not where a session starts; today is, but it brings the calendar, the sheet
// and dnd-kit's sortable machinery with it. Loading it on demand keeps those
// out of the shell's preload list: the shell paints, then the list arrives.
const TodayPage = lazy(() => import('./routes/TodayPage').then((m) => ({ default: m.TodayPage })));
const UpcomingPage = lazy(() => import('./routes/UpcomingPage'));
const HistoryPage = lazy(() => import('./routes/HistoryPage'));
const SearchPage = lazy(() => import('./routes/SearchPage'));
const SettingsPage = lazy(() => import('./routes/SettingsPage'));

const query = new QueryClient({
  defaultOptions: {
    queries: { retry: 1, refetchOnWindowFocus: true, staleTime: 15_000 },
  },
});

/** What a route shows for the instant its chunk is on the wire. */
function Loading() {
  return (
    <div className="space-y-2">
      <Skeleton className="h-5 w-24" />
      <Skeleton className="h-10 w-full" />
      <Skeleton className="h-10 w-full" />
    </div>
  );
}

// React Router, not TanStack Router: five routes do not pay for a code
// generation step, and the paths are the server's own ("/", "/upcoming"…),
// so a reload is still one HTTP request to Go, which answers with this shell
// for any path it does not own itself. /login is one of the paths Go keeps:
// the form has to work before any JavaScript has been built or trusted.
createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <QueryClientProvider client={query}>
      <BrowserRouter>
        <Suspense fallback={<Loading />}>
          <Routes>
            <Route element={<App />}>
              <Route index element={<TodayPage />} />
              <Route path="upcoming" element={<UpcomingPage />} />
              <Route path="history" element={<HistoryPage />} />
              <Route path="search" element={<SearchPage />} />
              <Route path="settings" element={<SettingsPage />} />
            </Route>
          </Routes>
        </Suspense>
      </BrowserRouter>
      <Toaster position="bottom-center" />
    </QueryClientProvider>
  </StrictMode>,
);
