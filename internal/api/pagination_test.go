package api

import (
	"encoding/json"
	"testing"
)

func TestPaginationUnmarshalAliases(t *testing.T) {
	cases := map[string]string{
		"page_size + current_page + total_count": `{"current_page":2,"page_size":10,"total_pages":5,"total_count":42}`,
		"page_number + per_page + total_records": `{"page_number":2,"per_page":10,"total_pages":5,"total_records":42}`,
		"page + total_entries":                   `{"page":2,"page_size":10,"total_pages":5,"total_entries":42}`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			var p Pagination
			if err := json.Unmarshal([]byte(body), &p); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if p.Page != 2 || p.PageSize != 10 || p.TotalPages != 5 || p.TotalCount != 42 {
				t.Errorf("got %+v", p)
			}
			if !p.HasInfo() {
				t.Error("HasInfo should be true")
			}
		})
	}
}

func TestPaginationHasInfoZero(t *testing.T) {
	var p Pagination
	if p.HasInfo() {
		t.Error("zero pagination should report no info")
	}
}
