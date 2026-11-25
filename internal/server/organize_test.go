package server

import (
	"context"
	"net/http"
	"testing"
	"today/internal/store"
)

func TestCategoryAndTrashAPI(t *testing.T) {
	e := newTestEnv(t)
	response := e.apiCall("GET", "/api/v2/categories", nil)
	response.Body.Close()
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatal("category list bypasses authentication")
	}
	e.login(e.password())
	var category store.Category
	e.apiJSON("POST", "/api/v2/categories", map[string]string{"name": "学习"}, &category)
	var item store.Item
	e.apiJSON("POST", "/api/v2/items", map[string]string{"title": "阅读", "category_id": category.ID}, &item)
	if item.CategoryID != category.ID {
		t.Fatal("create lost category")
	}
	e.apiJSON("DELETE", "/api/v2/items/"+item.ID, nil, nil)
	var trash []store.Item
	e.apiJSON("GET", "/api/v2/trash", nil, &trash)
	if len(trash) != 1 {
		t.Fatal("deleted item missing from trash")
	}
	e.apiJSON("POST", "/api/v2/items/"+item.ID+"/restore", nil, &item)
	if _, err := e.st.Item(context.Background(), item.ID); err != nil {
		t.Fatal(err)
	}
	e.apiJSON("DELETE", "/api/v2/categories/"+category.ID, nil, nil)
	live, err := e.st.Item(context.Background(), item.ID)
	if err != nil || live.CategoryID != "" {
		t.Fatal("deleting category damaged item", err)
	}
}
