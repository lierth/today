package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"html/template"
	"net/http"
	"regexp"
	"strings"
	"time"

	"today/internal/store"
)

type pageData struct {
	Title     string
	Nav       string
	DateLabel string
	Items     []store.Item
	All       []store.Item
	Item      store.Item
	HasItem   bool
	Past      []store.Item
	Settings  map[string]string
	Devices   []store.Device
	Stats     store.Stats
	Total     int
	Done      int
	Token     string
	Message   string
	Error     string
}

var weekdayZH = [...]string{"周日", "周一", "周二", "周三", "周四", "周五", "周六"}

func templateFuncs() template.FuncMap {
	return template.FuncMap{
		"timeLabel":   itemTimeLabel,
		"dateLabel":   itemDateLabel,
		"dateValue":   itemDateValue,
		"timeValue":   itemTimeValue,
		"statusLabel": itemStatusLabel,
		"fmtTime":     fmtTime,
	}
}

func itemTimeLabel(it store.Item) string {
	if it.AllDay {
		return "全天"
	}
	if it.StartsAt == nil {
		return "—"
	}
	return it.StartsAt.Format("15:04")
}

func itemDateLabel(it store.Item) string {
	t, err := time.Parse(store.DateLayout, it.DateLocal)
	if err != nil {
		return "—"
	}
	return t.Format("1月2日")
}

func itemDateValue(it store.Item) string {
	if it.DateLocal == "" {
		return ""
	}
	return it.DateLocal
}

func itemTimeValue(it store.Item) string {
	if it.AllDay || it.StartsAt == nil {
		return ""
	}
	return it.StartsAt.Format("15:04")
}

func itemStatusLabel(it store.Item) string {
	switch it.Status {
	case store.StatusDone:
		return "已完成"
	case store.StatusCancelled:
		return "已取消"
	}
	return "未完成"
}

func fmtTime(t *time.Time) string {
	if t == nil {
		return "—"
	}
	return t.Format("01-02 15:04")
}

func (s *Server) render(w http.ResponseWriter, r *http.Request, name string, data pageData) {
	s.renderStatus(w, r, http.StatusOK, name, data)
}

func (s *Server) renderStatus(w http.ResponseWriter, _ *http.Request, code int, name string, data pageData) {
	var buf bytes.Buffer
	if err := s.tmpl.ExecuteTemplate(&buf, name, data); err != nil {
		s.log.Printf("server: render %s: %v", name, err)
		http.Error(w, "template", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(code)
	_, _ = buf.WriteTo(w)
}

func (s *Server) handleApp(w http.ResponseWriter, r *http.Request) {
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
	s.render(w, r, "app.html", pageData{
		Title:     "今天",
		Nav:       "today",
		DateLabel: day.Format("1月2日") + " " + weekdayZH[day.Weekday()],
		Items:     items,
		Total:     total,
		Done:      done,
	})
}

func (s *Server) handleItemPage(w http.ResponseWriter, r *http.Request) {
	it, err := s.st.Item(r.Context(), r.PathValue("id"))
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		http.Error(w, "db", http.StatusInternalServerError)
		return
	}
	s.render(w, r, "item.html", pageData{Title: it.Title, Nav: "items", Item: it, HasItem: true})
}

func (s *Server) handleItemsPage(w http.ResponseWriter, r *http.Request) {
	items, err := s.st.AllItems(r.Context(), 500)
	if err != nil {
		http.Error(w, "db", http.StatusInternalServerError)
		return
	}
	s.render(w, r, "items.html", pageData{Title: "事项", Nav: "items", All: items})
}

func (s *Server) handleHistory(w http.ResponseWriter, r *http.Request) {
	day, _, err := s.Today(r.Context())
	if err != nil {
		http.Error(w, "timezone", http.StatusInternalServerError)
		return
	}
	past, err := s.st.PastItems(r.Context(), day.Format(store.DateLayout), 200)
	if err != nil {
		http.Error(w, "db", http.StatusInternalServerError)
		return
	}
	s.render(w, r, "history.html", pageData{Title: "历史", Nav: "history", Past: past})
}

func (s *Server) settingsData(ctx context.Context) (pageData, error) {
	st, err := s.st.Settings(ctx)
	if err != nil {
		return pageData{}, err
	}
	devices, err := s.st.Devices(ctx)
	if err != nil {
		return pageData{}, err
	}
	day, _, err := s.Today(ctx)
	if err != nil {
		return pageData{}, err
	}
	stats, err := s.st.Stats(ctx, day.Format(store.DateLayout))
	if err != nil {
		return pageData{}, err
	}
	return pageData{
		Title:    "设置",
		Nav:      "settings",
		Settings: st,
		Devices:  devices,
		Stats:    stats,
	}, nil
}

func (s *Server) handleSettingsPage(w http.ResponseWriter, r *http.Request) {
	data, err := s.settingsData(r.Context())
	if err != nil {
		http.Error(w, "db", http.StatusInternalServerError)
		return
	}
	s.render(w, r, "settings.html", data)
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func (s *Server) handleItemsList(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var (
		items []store.Item
		err   error
	)
	if r.URL.Query().Get("scope") == "all" {
		items, err = s.st.AllItems(ctx, 500)
	} else {
		day, _, derr := s.Today(ctx)
		if derr != nil {
			http.Error(w, "timezone", http.StatusInternalServerError)
			return
		}
		items, err = s.st.ItemsOn(ctx, day.Format(store.DateLayout))
	}
	if err != nil {
		http.Error(w, "db", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func isJSON(r *http.Request) bool {
	return strings.Contains(r.Header.Get("Content-Type"), "application/json")
}

func (s *Server) handleItemCreate(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	loc, err := s.Location(ctx)
	if err != nil {
		http.Error(w, "timezone", http.StatusInternalServerError)
		return
	}
	day := time.Now().In(loc)
	today := day.Format(store.DateLayout)
	var in store.Item
	if isJSON(r) {
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			http.Error(w, "bad json", http.StatusBadRequest)
			return
		}
	} else {
		if err := r.ParseForm(); err != nil {
			http.Error(w, "bad form", http.StatusBadRequest)
			return
		}
		in.Title = r.FormValue("title")
		in.Description = r.FormValue("description")
		in.RRule = strings.TrimSpace(r.FormValue("rrule"))
		date := strings.TrimSpace(r.FormValue("date"))
		clock := strings.TrimSpace(r.FormValue("time"))
		if date == "" {
			date = today
		}
		if clock == "" && in.RRule != "" {
			clock = "09:00"
		}
		if clock != "" {
			start, err := time.ParseInLocation("2006-01-02 15:04", date+" "+clock, loc)
			if err != nil {
				http.Error(w, "bad time", http.StatusBadRequest)
				return
			}
			in.StartsAt = &start
		} else {
			in.AllDay = true
			in.DateLocal = date
		}
	}
	item, err := s.st.CreateItem(ctx, in, loc)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if item.RRule != "" {
		if _, err := s.st.Materialize(ctx, loc, time.Now(), 31); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}
	if isJSON(r) {
		writeJSON(w, http.StatusCreated, item)
		return
	}
	view := r.FormValue("view")
	if view == "" {
		view = "today"
	}
	if r.Header.Get("HX-Request") == "true" {
		s.respondItems(w, r, view)
		return
	}
	target := "/app"
	if view == "items" {
		target = "/items"
	}
	http.Redirect(w, r, target, http.StatusSeeOther)
}

func (s *Server) handleItemPatch(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := r.PathValue("id")
	loc, err := s.Location(ctx)
	if err != nil {
		http.Error(w, "timezone", http.StatusInternalServerError)
		return
	}
	var p store.Patch
	view := "today"
	if isJSON(r) {
		if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
			http.Error(w, "bad json", http.StatusBadRequest)
			return
		}
	} else {
		if err := r.ParseForm(); err != nil {
			http.Error(w, "bad form", http.StatusBadRequest)
			return
		}
		form := r.Form
		view = form.Get("view")
		full := form.Get("full") == "1"
		if form.Has("title") {
			v := form.Get("title")
			p.Title = &v
		}
		if form.Has("description") {
			v := form.Get("description")
			p.Description = &v
		}
		if form.Has("status") {
			v := form.Get("status")
			p.Status = &v
		}
		if full || form.Has("pinned") {
			v := form.Get("pinned") != ""
			p.Pinned = &v
		}
		if full || form.Has("all_day") {
			v := form.Get("all_day") != ""
			p.AllDay = &v
		}
		if form.Has("rrule") {
			v := form.Get("rrule")
			p.RRule = &v
		}
		if full || form.Has("date") || form.Has("time") {
			date := strings.TrimSpace(form.Get("date"))
			clock := strings.TrimSpace(form.Get("time"))
			if date == "" {
				date = time.Now().In(loc).Format(store.DateLayout)
			}
			if clock == "" {
				v := ""
				p.StartsAt = &v
			} else {
				start, err := time.ParseInLocation("2006-01-02 15:04", date+" "+clock, loc)
				if err != nil {
					http.Error(w, "bad time", http.StatusBadRequest)
					return
				}
				v := timeStr(start)
				p.StartsAt = &v
			}
			p.DateLocal = &date
		}
	}
	item, err := s.st.UpdateItem(ctx, id, p, loc)
	if err != nil {
		code := http.StatusBadRequest
		if errors.Is(err, store.ErrNotFound) {
			code = http.StatusNotFound
		}
		http.Error(w, err.Error(), code)
		return
	}
	if isJSON(r) {
		writeJSON(w, http.StatusOK, item)
		return
	}
	if view == "" {
		view = "today"
	}
	if view == "item" && r.Header.Get("HX-Request") != "true" {
		http.Redirect(w, r, "/items", http.StatusSeeOther)
		return
	}
	s.respondItems(w, r, view)
}

func (s *Server) handleItemDelete(w http.ResponseWriter, r *http.Request) {
	if err := s.st.DeleteItem(r.Context(), r.PathValue("id")); err != nil {
		code := http.StatusBadRequest
		if errors.Is(err, store.ErrNotFound) {
			code = http.StatusNotFound
		}
		http.Error(w, err.Error(), code)
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
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleItemDeletePost(w http.ResponseWriter, r *http.Request) {
	if err := s.st.DeleteItem(r.Context(), r.PathValue("id")); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	http.Redirect(w, r, "/items", http.StatusSeeOther)
}

// respondItems re-renders the list partial the caller targeted.
func (s *Server) respondItems(w http.ResponseWriter, r *http.Request, view string) {
	ctx := r.Context()
	if view == "items" {
		items, err := s.st.AllItems(ctx, 500)
		if err != nil {
			http.Error(w, "db", http.StatusInternalServerError)
			return
		}
		s.render(w, r, "items_list.html", pageData{All: items})
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
	s.render(w, r, "today_list.html", pageData{Items: items})
}

func (s *Server) handleSettingsGet(w http.ResponseWriter, r *http.Request) {
	st, err := s.st.Settings(r.Context())
	if err != nil {
		http.Error(w, "db", http.StatusInternalServerError)
		return
	}
	out := make(map[string]string, len(st))
	for k, v := range st {
		switch k {
		case "auth_password_hash", "auth_session_key":
			continue
		}
		out[k] = v
	}
	writeJSON(w, http.StatusOK, out)
}

var deviceIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,31}$`)

func (s *Server) handleSettingsPost(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data") {
		r.Body = http.MaxBytesReader(w, r.Body, maxFirmwareBytes+(1<<20))
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			s.renderSettingsPanel(w, r, pageData{Error: "上传内容过大或格式错误"}, http.StatusBadRequest)
			return
		}
	} else if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	action := r.FormValue("action")
	var msg string
	switch action {
	case "upload_firmware":
		version, err := s.saveFirmware(r)
		if err != nil {
			s.renderSettingsPanel(w, r, pageData{Error: err.Error()}, http.StatusBadRequest)
			return
		}
		msg = "固件已上传：" + version + "，设备下次取帧后升级"
	case "password":
		pw := r.FormValue("new_password")
		if err := s.SetPassword(ctx, pw); err != nil {
			s.renderSettingsPanel(w, r, pageData{Error: err.Error()}, http.StatusBadRequest)
			return
		}
		if r.Header.Get("HX-Request") == "true" {
			w.Header().Set("HX-Redirect", "/login")
			w.WriteHeader(http.StatusOK)
			return
		}
		s.clearSessionCookie(w, r)
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	case "logout_all":
		if err := s.RotateSessionKey(ctx); err != nil {
			s.renderSettingsPanel(w, r, pageData{Error: err.Error()}, http.StatusInternalServerError)
			return
		}
		s.clearSessionCookie(w, r)
		if r.Header.Get("HX-Request") == "true" {
			w.Header().Set("HX-Redirect", "/login")
			w.WriteHeader(http.StatusOK)
			return
		}
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	case "add_device":
		id := strings.ToLower(strings.TrimSpace(r.FormValue("device_id")))
		name := strings.TrimSpace(r.FormValue("device_name"))
		if !deviceIDPattern.MatchString(id) {
			s.renderSettingsPanel(w, r, pageData{Error: "设备 ID 只能用小写字母、数字、- 和 _（1–32 位）。"}, http.StatusBadRequest)
			return
		}
		if name == "" {
			name = id
		}
		if _, err := s.st.Device(ctx, id); err == nil {
			s.renderSettingsPanel(w, r, pageData{Error: "设备 " + id + " 已存在。"}, http.StatusBadRequest)
			return
		}
		if _, err := s.st.CreateDevice(ctx, id, name, ""); err != nil {
			s.renderSettingsPanel(w, r, pageData{Error: err.Error()}, http.StatusInternalServerError)
			return
		}
		msg = "设备已添加，接着为它生成 token。"
	case "device_token":
		id := r.FormValue("device")
		token := newDeviceToken()
		if err := s.st.SetDeviceToken(ctx, id, tokenHash(token)); err != nil {
			s.renderSettingsPanel(w, r, pageData{Error: err.Error()}, http.StatusBadRequest)
			return
		}
		hint := token
		if len(hint) > 4 {
			hint = "…" + hint[len(hint)-4:]
		}
		_, _ = s.UpdateSettings(ctx, map[string]string{"device_token_hint": hint})
		msg = "新 token 已生成，旧 token 立即失效。"
		data, err := s.settingsData(ctx)
		if err != nil {
			http.Error(w, "db", http.StatusInternalServerError)
			return
		}
		data.Token = token
		data.Message = msg
		s.render(w, r, "settings_panel.html", data)
		return
	case "revoke_device":
		id := r.FormValue("device")
		if err := s.st.RevokeDevice(ctx, id); err != nil {
			s.renderSettingsPanel(w, r, pageData{Error: err.Error()}, http.StatusBadRequest)
			return
		}
		msg = "设备已吊销。"
	default:
		tz := strings.TrimSpace(r.FormValue("timezone"))
		if tz != "" {
			if _, err := time.LoadLocation(tz); err != nil {
				s.renderSettingsPanel(w, r, pageData{Error: "时区无效：" + tz}, http.StatusBadRequest)
				return
			}
		}
		feed := strings.TrimSpace(r.FormValue("update_feed"))
		if feed != "" && !strings.HasPrefix(feed, "https://") && !strings.HasPrefix(feed, "http://") {
			s.renderSettingsPanel(w, r, pageData{Error: "更新源必须是 http(s) 地址。"}, http.StatusBadRequest)
			return
		}
		diff := map[string]string{
			"open_at_login": boolString(r.FormValue("open_at_login") != ""),
			"tray_title":    boolString(r.FormValue("tray_title") != ""),
			"funnel":        boolString(r.FormValue("funnel") != ""),
		}
		if _, ok := r.Form["update_feed"]; ok {
			diff["update_feed"] = feed
		}
		if tz != "" {
			diff["timezone"] = tz
		}
		if _, err := s.UpdateSettings(ctx, diff); err != nil {
			s.renderSettingsPanel(w, r, pageData{Error: err.Error()}, http.StatusBadRequest)
			return
		}
		msg = "设置已保存。"
	}
	data, err := s.settingsData(ctx)
	if err != nil {
		http.Error(w, "db", http.StatusInternalServerError)
		return
	}
	data.Message = msg
	s.render(w, r, "settings_panel.html", data)
}

func (s *Server) renderSettingsPanel(w http.ResponseWriter, r *http.Request, data pageData, code int) {
	full, err := s.settingsData(r.Context())
	if err != nil {
		http.Error(w, "db", http.StatusInternalServerError)
		return
	}
	if data.Error != "" {
		full.Error = data.Error
	}
	if data.Message != "" {
		full.Message = data.Message
	}
	if data.Token != "" {
		full.Token = data.Token
	}
	s.renderStatus(w, r, code, "settings_panel.html", full)
}

func boolString(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

func timeStr(t time.Time) string { return t.Format(time.RFC3339Nano) }
