/*
Copyright © 2026 NervesHub

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in
all copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN
THE SOFTWARE.
*/

package api

import "encoding/json"

// Pagination is the page metadata returned alongside paginated list responses.
//
// The NervesCloud pagination payload is undocumented and field names differ
// between conventions, so UnmarshalJSON accepts several common aliases for each
// value. Unknown shapes simply leave the field at zero rather than erroring.
//
// TODO: pin the exact field names against a real paginated response.
type Pagination struct {
	Page       int `json:"page"`
	PageSize   int `json:"page_size"`
	TotalPages int `json:"total_pages"`
	TotalCount int `json:"total_count"`
}

// UnmarshalJSON maps the various pagination key names onto Pagination.
func (p *Pagination) UnmarshalJSON(b []byte) error {
	var a struct {
		Page         *int `json:"page"`
		PageNumber   *int `json:"page_number"`
		CurrentPage  *int `json:"current_page"`
		PageSize     *int `json:"page_size"`
		PerPage      *int `json:"per_page"`
		TotalPages   *int `json:"total_pages"`
		TotalCount   *int `json:"total_count"`
		TotalRecords *int `json:"total_records"`
		TotalEntries *int `json:"total_entries"`
		Total        *int `json:"total"`
	}
	if err := json.Unmarshal(b, &a); err != nil {
		return err
	}
	p.Page = firstInt(a.CurrentPage, a.PageNumber, a.Page)
	p.PageSize = firstInt(a.PageSize, a.PerPage)
	p.TotalPages = firstInt(a.TotalPages)
	p.TotalCount = firstInt(a.TotalCount, a.TotalRecords, a.TotalEntries, a.Total)
	return nil
}

// HasInfo reports whether the pagination carries any meaningful data worth
// displaying.
func (p Pagination) HasInfo() bool {
	return p.Page > 0 || p.TotalPages > 0 || p.TotalCount > 0
}

// firstInt returns the value of the first non-nil pointer, or 0.
func firstInt(vals ...*int) int {
	for _, v := range vals {
		if v != nil {
			return *v
		}
	}
	return 0
}
