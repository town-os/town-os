package systemcontroller

import (
	"fmt"
	"net/url"
	"reflect"
	"strconv"
	"strings"

	"github.com/labstack/echo/v5"
)

// ListParams holds sorting, pagination, and search parameters for list endpoints.
type ListParams struct {
	SortBy        string `json:"sort_by"`
	SortOrder     string `json:"sort_order"`
	Limit         int    `json:"limit"`
	Offset        int    `json:"offset"`
	Search        string `json:"search"`
	InstalledOnly bool   `json:"installed_only"`
	FeaturedOnly  bool   `json:"featured_only"`
}

// PageResult wraps a paginated slice of entries with metadata.
type PageResult[T any] struct {
	Entries    []T  `json:"entries"`
	HasMore    bool `json:"has_more"`
	TotalPages int  `json:"total_pages"`
	TotalCount int  `json:"total_count"`
}

const defaultPageLimit = 20

// paginate returns a PageResult for the given slice, limit, and offset.
// A limit of 0 means use the default (defaultPageLimit).
func paginate[T any](items []T, limit, offset int) PageResult[T] {
	total := len(items)

	if limit <= 0 {
		limit = defaultPageLimit
	}

	if offset < 0 {
		offset = 0
	}
	if offset > total {
		offset = total
	}

	end := min(offset+limit, total)

	totalPages := (total + limit - 1) / limit
	if totalPages == 0 {
		totalPages = 1
	}

	entries := items[offset:end]
	if entries == nil {
		entries = []T{}
	}

	return PageResult[T]{
		Entries:    entries,
		HasMore:    end < total,
		TotalPages: totalPages,
		TotalCount: total,
	}
}

// readListParams extracts sort, pagination, and search parameters from GET query parameters.
func readListParams(c *echo.Context) ListParams {
	limit, err := strconv.Atoi(c.QueryParam("limit"))
	if err != nil {
		limit = 0
	}
	offset, err := strconv.Atoi(c.QueryParam("offset"))
	if err != nil {
		offset = 0
	}
	return ListParams{
		SortBy:        c.QueryParam("sort_by"),
		SortOrder:     c.QueryParam("sort_order"),
		Limit:         limit,
		Offset:        offset,
		Search:        c.QueryParam("search"),
		InstalledOnly: c.QueryParam("installed_only") == "true",
		FeaturedOnly:  c.QueryParam("featured_only") == "true",
	}
}

// QueryString encodes the ListParams as a URL query string (including leading "?").
// Returns empty string if all fields are zero values.
func (p ListParams) QueryString() string {
	params := url.Values{}
	if p.SortBy != "" {
		params.Set("sort_by", p.SortBy)
	}
	if p.SortOrder != "" {
		params.Set("sort_order", p.SortOrder)
	}
	if p.Limit > 0 {
		params.Set("limit", strconv.Itoa(p.Limit))
	}
	if p.Offset > 0 {
		params.Set("offset", strconv.Itoa(p.Offset))
	}
	if p.Search != "" {
		params.Set("search", p.Search)
	}
	if p.InstalledOnly {
		params.Set("installed_only", "true")
	}
	if p.FeaturedOnly {
		params.Set("featured_only", "true")
	}
	if len(params) == 0 {
		return ""
	}
	return "?" + params.Encode()
}

// filterSearch returns items matching the search term by doing a case-insensitive
// substring match across all string fields of each item. For string slices, the
// match is against the string value itself.
func filterSearch[T any](items []T, search string) []T {
	if search == "" {
		return items
	}

	term := strings.ToLower(search)
	var out []T

	for _, item := range items {
		if matchesSearch(item, term) {
			out = append(out, item)
		}
	}

	if out == nil {
		return []T{}
	}
	return out
}

func matchesSearch[T any](item T, term string) bool {
	v := reflect.ValueOf(item)
	return matchesSearchValue(v, term)
}

func matchesSearchValue(v reflect.Value, term string) bool {
	if v.Kind() == reflect.String {
		return strings.Contains(strings.ToLower(v.String()), term)
	}

	if v.Kind() != reflect.Struct {
		return strings.Contains(strings.ToLower(fmt.Sprint(v.Interface())), term)
	}

	for i := range v.NumField() {
		f := v.Field(i)
		switch f.Kind() {
		case reflect.String:
			if strings.Contains(strings.ToLower(f.String()), term) {
				return true
			}
		case reflect.Struct:
			if matchesSearchValue(f, term) {
				return true
			}
		}
	}

	return false
}
