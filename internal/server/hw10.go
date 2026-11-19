package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"today/internal/frame"
	"today/internal/store"
)

// handleCarry moves past items to today. `ids` may be one item or many; the
// page posts either form. It answers with the rendered lists so HTMX can swap
// both the carried block and the day's list in one round trip.
func (s *Server) handleCarry(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	loc, err := s.Location(ctx)
	if err != nil {
		http.Error(w, "timezone", http.StatusInternalServerError)
		return
	}
	day, _, err := s.Today(ctx)
	if err != nil {
		http.Error(w, "timezone", http.StatusInternalServerError)
		return
	}
	ids := r.Form["item"]
	if len(ids) == 0 {
		ids = r.Form["id"]
	}
	if all := r.FormValue("all"); all != "" {
		unfinished, err := s.st.Unfinished(ctx, day.Format(store.DateLayout), loc)
		if err != nil {
			http.Error(w, "db", http.StatusInternalServerError)
			return
		}
		ids = ids[:0]
		for _, it := range unfinished {
			ids = append(ids, it.ID)
		}
	}
	if len(ids) == 0 {
		http.Error(w, "nothing to carry", http.StatusBadRequest)
		return
	}
	res, err := s.st.CarryItems(ctx, ids, day.Format(store.DateLayout), loc)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if res.Moved > 0 {
		s.ForceFullRefresh()
		// The toast offers the exact inverse: the rows keep carried_from, so
		// UncarryItems can put each back on its own day.
		var movedIDs []string
		for _, id := range ids {
			if _, skipped := res.Skipped[id]; !skipped {
				movedIDs = append(movedIDs, id)
			}
		}
		if len(movedIDs) > 0 {
			var b strings.Builder
			b.WriteString("/api/items/uncarry?view=today")
			for _, id := range movedIDs {
				b.WriteString("&item=" + url.QueryEscape(id))
			}
			text := "已移到今天"
			if res.Moved > 1 {
				text = "已移到今天（" + itoa(res.Moved) + " 项）"
			}
			w.Header().Set("HX-Trigger", undoTrigger(text, b.String()))
		}
	}
	s.renderCarry(w, r, res)
}

// handleUncarry is the undo of a carry: the rows go back to the day they
// came from. The toast for "已移到今天" points here with the ids in `item`.
func (s *Server) handleUncarry(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	loc, err := s.Location(ctx)
	if err != nil {
		http.Error(w, "timezone", http.StatusInternalServerError)
		return
	}
	ids := r.Form["item"]
	if len(ids) == 0 {
		ids = r.Form["id"]
	}
	if len(ids) == 0 {
		http.Error(w, "nothing to uncarry", http.StatusBadRequest)
		return
	}
	n, err := s.st.UncarryItems(ctx, ids, loc)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if n > 0 {
		s.ForceFullRefresh()
	}
	view := r.URL.Query().Get("view")
	if view == "" {
		view = "today"
	}
	if isJSON(r) {
		writeJSON(w, http.StatusOK, map[string]int{"moved": n})
		return
	}
	s.respondItems(w, r, view)
}

// renderCarry answers with the two sections the carry buttons touch: the
// remaining unfinished list and today's list.
func (s *Server) renderCarry(w http.ResponseWriter, r *http.Request, res store.CarryResult) {
	ctx := r.Context()
	loc, err := s.Location(ctx)
	if err != nil {
		http.Error(w, "timezone", http.StatusInternalServerError)
		return
	}
	day, _, err := s.Today(ctx)
	if err != nil {
		http.Error(w, "timezone", http.StatusInternalServerError)
		return
	}
	items, err := s.st.ItemsOn(ctx, day.Format(store.DateLayout))
	if err != nil {
		http.Error(w, "db", http.StatusInternalServerError)
		return
	}
	carried, err := s.st.Unfinished(ctx, day.Format(store.DateLayout), loc)
	if err != nil {
		http.Error(w, "db", http.StatusInternalServerError)
		return
	}
	rows := make([]carryRow, 0, len(carried))
	for _, it := range carried {
		rows = append(rows, carryRow{Item: it, DayLabel: chineseDay(it.DateLocal, loc)})
	}
	total, done := 0, 0
	for _, it := range items {
		if it.Status == store.StatusCancelled {
			continue
		}
		total++
		if it.Done() {
			done++
		}
	}
	s.render(w, r, "carry_result.html", pageData{
		Items:       items,
		Total:       total,
		Done:        done,
		HasProgress: true,
		DateLabel:   day.Format("1月2日") + " " + weekdayZH[day.Weekday()],
		CarriedAt:   rows,
		Message:     carryMessage(res),
		BoardLimit:  frame.BoardRowLimit,
	})
}

// carryMessage says what moved and, when some rows were refused, why. The
// skipped reasons are one sentence each, deduplicated.
func carryMessage(res store.CarryResult) string {
	var b strings.Builder
	switch res.Moved {
	case 0:
	case 1:
		b.WriteString("已移到今天。")
	default:
		b.WriteString("已移到今天（" + itoa(res.Moved) + " 项）。")
	}
	seen := map[string]bool{}
	for _, reason := range res.Skipped {
		if seen[reason] {
			continue
		}
		seen[reason] = true
		if b.Len() > 0 {
			b.WriteString(" ")
		}
		b.WriteString("有 " + itoa(countReason(res.Skipped, reason)) + " 项未移动：" + reason + "。")
	}
	return b.String()
}

func countReason(skipped map[string]string, reason string) int {
	n := 0
	for _, r := range skipped {
		if r == reason {
			n++
		}
	}
	return n
}

// chineseDay renders a local date as 11月12日, falling back to the raw value.
func chineseDay(date string, loc *time.Location) string {
	t, err := time.ParseInLocation(store.DateLayout, date, loc)
	if err != nil {
		return date
	}
	return t.Format("1月2日")
}

// handleReorder writes one drag order. The body is either repeated `item`
// fields in the new order or a JSON array of ids.
func (s *Server) handleReorder(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	loc, err := s.Location(ctx)
	if err != nil {
		http.Error(w, "timezone", http.StatusInternalServerError)
		return
	}
	var ids []string
	if isJSON(r) {
		var body struct {
			Items []string `json:"items"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "bad json", http.StatusBadRequest)
			return
		}
		ids = body.Items
	} else {
		if err := r.ParseForm(); err != nil {
			http.Error(w, "bad form", http.StatusBadRequest)
			return
		}
		ids = r.Form["item"]
	}
	if len(ids) == 0 {
		http.Error(w, "no order", http.StatusBadRequest)
		return
	}
	if err := s.st.Reorder(ctx, ids, loc); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	s.ForceFullRefresh()
	s.renderTodayList(w, r)
}

// handleRestore undoes a soft delete (the undo toast's action).
func (s *Server) handleRestore(w http.ResponseWriter, r *http.Request) {
	item, err := s.st.RestoreItem(r.Context(), r.PathValue("id"))
	if err != nil {
		code := http.StatusBadRequest
		if errors.Is(err, store.ErrNotFound) {
			code = http.StatusNotFound
		}
		http.Error(w, err.Error(), code)
		return
	}
	s.ForceFullRefresh()
	if isJSON(r) {
		writeJSON(w, http.StatusOK, item)
		return
	}
	view := r.URL.Query().Get("view")
	if view == "" {
		view = r.FormValue("view")
	}
	if r.Header.Get("HX-Request") == "true" {
		s.respondItems(w, r, view)
		return
	}
	http.Redirect(w, r, "/app", http.StatusSeeOther)
}

// handleStatusUndo restores the status a tick replaced. The page's undo toast
// posts here with the state that was shown before the tick.
func (s *Server) handleStatusUndo(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := r.PathValue("id")
	status := r.PathValue("status")
	switch status {
	case store.StatusOpen, store.StatusDone, store.StatusCancelled:
	default:
		http.Error(w, "bad status", http.StatusBadRequest)
		return
	}
	loc, err := s.Location(ctx)
	if err != nil {
		http.Error(w, "timezone", http.StatusInternalServerError)
		return
	}
	title := r.URL.Query().Get("title")
	if title != "" {
		// The undo of an "add" deletes the row instead of resetting a status;
		// that path is /api/items/{id}/delete. Here the title is ignored.
	}
	if _, err := s.st.UpdateItem(ctx, id, store.Patch{Status: &status}, loc); err != nil {
		code := http.StatusBadRequest
		if errors.Is(err, store.ErrNotFound) {
			code = http.StatusNotFound
		}
		http.Error(w, err.Error(), code)
		return
	}
	s.ForceFullRefresh()
	view := r.URL.Query().Get("view")
	if view == "" {
		view = "today"
	}
	if r.Header.Get("HX-Request") == "true" {
		s.respondItems(w, r, view)
		return
	}
	http.Redirect(w, r, "/app", http.StatusSeeOther)
}

// handleParsePreview answers the quick-add tag strip. It never writes.
func (s *Server) handleParsePreview(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	loc, err := s.Location(ctx)
	if err != nil {
		http.Error(w, "timezone", http.StatusInternalServerError)
		return
	}
	// The box posts the free-form line as `raw`; `title` is accepted too so a
	// caller can ask about any string.
	text := r.FormValue("raw")
	if text == "" {
		text = r.FormValue("title")
	}
	got := ParseQuickAdd(text, s.clock(), loc)
	writeJSON(w, http.StatusOK, got)
}

// handleSearch renders the search results page, grouped by date.
func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	items, err := s.st.Search(ctx, query, 100)
	if err != nil {
		http.Error(w, "db", http.StatusInternalServerError)
		return
	}
	loc, err := s.Location(ctx)
	if err != nil {
		http.Error(w, "timezone", http.StatusInternalServerError)
		return
	}
	groups := make([]upcomingGroup, 0)
	index := map[string]int{}
	for _, it := range items {
		label := chineseDay(it.DateLocal, loc)
		i, ok := index[it.DateLocal]
		if !ok {
			i = len(groups)
			index[it.DateLocal] = i
			groups = append(groups, upcomingGroup{Label: label})
		}
		groups[i].Items = append(groups[i].Items, it)
	}
	s.render(w, r, "search.html", pageData{
		Title: "搜索", Nav: "items", Query: query, Results: groups,
	})
}

// handleReorderGroup is the keyboard/phone fallback: move one item one place
// up or down inside today's list, then rewrite the whole order so the result
// is stable regardless of the starting state.
func (s *Server) handleReorderGroup(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	loc, err := s.Location(ctx)
	if err != nil {
		http.Error(w, "timezone", http.StatusInternalServerError)
		return
	}
	day, _, err := s.Today(ctx)
	if err != nil {
		http.Error(w, "timezone", http.StatusInternalServerError)
		return
	}
	items, err := s.st.ItemsOn(ctx, day.Format(store.DateLayout))
	if err != nil {
		http.Error(w, "db", http.StatusInternalServerError)
		return
	}
	id := r.FormValue("item")
	dir := r.FormValue("dir")
	pos := -1
	for i, it := range items {
		if it.ID == id {
			pos = i
			break
		}
	}
	if pos < 0 {
		http.Error(w, "item not in today", http.StatusNotFound)
		return
	}
	target := pos - 1
	if dir == "down" {
		target = pos + 1
	}
	if dir != "up" && dir != "down" {
		http.Error(w, "bad dir", http.StatusBadRequest)
		return
	}
	if target < 0 || target >= len(items) {
		// Already at the edge: nothing moves, the answer is the same list.
		// Calling handleReorder here would read no ids and 400, leaving the
		// row looking like it jumped.
		s.renderTodayList(w, r)
		return
	}
	items[pos], items[target] = items[target], items[pos]
	ids := make([]string, len(items))
	for i, it := range items {
		ids[i] = it.ID
	}
	if err := s.st.Reorder(ctx, ids, loc); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	s.ForceFullRefresh()
	s.respondItems(w, r, "today")
}

// renderTodayList answers with the current list and nothing else. It is the
// no-op answer for a move that cannot happen.
func (s *Server) renderTodayList(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	day, _, err := s.Today(ctx)
	if err != nil {
		http.Error(w, "timezone", http.StatusInternalServerError)
		return
	}
	items, err := s.st.ItemsOn(ctx, day.Format(store.DateLayout))
	if err != nil {
		http.Error(w, "db", http.StatusInternalServerError)
		return
	}
	total, done := 0, 0
	for _, it := range items {
		if it.Status == store.StatusCancelled {
			continue
		}
		total++
		if it.Done() {
			done++
		}
	}
	s.render(w, r, "today_list.html", pageData{
		Items: items, Total: total, Done: done, BoardLimit: frame.BoardRowLimit,
	})
}
