package server

import (
	"net/http"
)

func (s *Server) registerOrganizeAPI(m *http.ServeMux) {
	m.HandleFunc("GET /api/v2/categories", s.requireSession(func(w http.ResponseWriter, r *http.Request) {
		rows, err := s.st.Categories(r.Context())
		if err != nil {
			writeAPIError(w, 500, "分类读取失败")
			return
		}
		writeJSON(w, 200, rows)
	}))
	save := func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Name string `json:"name"`
		}
		if !decodeJSON(w, r, &body) {
			return
		}
		row, err := s.st.SaveCategory(r.Context(), r.PathValue("id"), body.Name)
		if err != nil {
			writeAPIError(w, 400, s.explain(err, "分类保存失败"))
			return
		}
		writeJSON(w, 200, row)
	}
	m.HandleFunc("POST /api/v2/categories", s.requireSession(save))
	m.HandleFunc("PATCH /api/v2/categories/{id}", s.requireSession(save))
	m.HandleFunc("DELETE /api/v2/categories/{id}", s.requireSession(func(w http.ResponseWriter, r *http.Request) {
		if err := s.st.DeleteCategory(r.Context(), r.PathValue("id")); err != nil {
			writeAPIError(w, 400, s.explain(err, "分类删除失败"))
			return
		}
		writeJSON(w, 200, map[string]bool{"ok": true})
	}))
	m.HandleFunc("GET /api/v2/trash", s.requireSession(func(w http.ResponseWriter, r *http.Request) {
		rows, err := s.st.Trash(r.Context())
		if err != nil {
			writeAPIError(w, 500, "回收站读取失败")
			return
		}
		writeJSON(w, 200, rows)
	}))
	m.HandleFunc("DELETE /api/v2/trash/{id}", s.requireSession(func(w http.ResponseWriter, r *http.Request) {
		if err := s.st.DeletePermanently(r.Context(), r.PathValue("id")); err != nil {
			writeAPIError(w, 500, "永久删除失败")
			return
		}
		writeJSON(w, 200, map[string]bool{"ok": true})
	}))
}
