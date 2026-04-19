package systemcontroller

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"gitea.com/town-os/town-os/src/systemd"
)

// --- Systemd ---

// ListUnits returns a paginated list of systemd service units.
func (c *SystemdClient) ListUnits(ctx context.Context, params ListParams) (_ *PageResult[UnitListEntry], err error) {
	resp, err := c.getClient(ctx, "systemd/units"+params.QueryString())
	if err != nil {
		return nil, fmt.Errorf("%w: ListUnits: %w", ErrHTTPRequest, err)
	}
	defer func() {
		err = errors.Join(err, resp.Body.Close())
	}()

	if resp.StatusCode != http.StatusOK {
		return nil, readProblemDetail(resp, "GET", "systemd/units")
	}

	var page PageResult[UnitListEntry]
	return &page, json.NewDecoder(resp.Body).Decode(&page)
}

// SetUnitStatus applies a status action (start, stop, restart) to a systemd unit.
func (c *SystemdClient) SetUnitStatus(ctx context.Context, name string, action systemd.StatusAction) error {
	pr, pw := io.Pipe()
	go pipeEncode(pw, SetStatusRequest{Name: name, Action: action})

	return c.postClient(ctx, "systemd/status", pr)
}

// ListUnitsTree returns package service units grouped into a dependency tree.
func (c *SystemdClient) ListUnitsTree(ctx context.Context, params ListParams) (_ *UnitTreeResponse, err error) {
	resp, err := c.getClient(ctx, "systemd/units-tree"+params.QueryString())
	if err != nil {
		return nil, fmt.Errorf("%w: ListUnitsTree: %w", ErrHTTPRequest, err)
	}
	defer func() {
		err = errors.Join(err, resp.Body.Close())
	}()

	if resp.StatusCode != http.StatusOK {
		return nil, readProblemDetail(resp, "GET", "systemd/units-tree")
	}

	var out UnitTreeResponse
	return &out, json.NewDecoder(resp.Body).Decode(&out)
}

// SetUnitStatusTree cascades a status action across a package and all its
// installed dependencies.
func (c *SystemdClient) SetUnitStatusTree(ctx context.Context, repo, name, version string, action systemd.StatusAction) error {
	pr, pw := io.Pipe()
	go pipeEncode(pw, TreeStatusRequest{Repo: repo, Name: name, Version: version, Action: action})

	return c.postClient(ctx, "systemd/status/tree", pr)
}

// LogReplay streams historical journal entries for a unit via server-sent
// events. The returned channel is closed when the replay completes.
func (c *SystemdClient) LogReplay(ctx context.Context, name string) (<-chan systemd.JournalEntry, error) {
	return c.streamLogReplay(ctx, "systemd/logs?unit="+url.QueryEscape(name), "systemd/logs")
}

// LogTail returns a page of recent journal entries with cursor-based pagination.
func (c *SystemdClient) LogTail(ctx context.Context, p systemd.LogTailParams) (_ systemd.LogTailResult, err error) {
	q := fmt.Sprintf("systemd/logs/tail?unit=%s&lines=%d", url.QueryEscape(p.Unit), p.Lines)
	return c.fetchLogTail(ctx, appendLogTailQuery(q, p), "systemd/logs/tail")
}

// LogReplayTree streams the merged journal history for every systemd unit
// in a package's dependency tree.
func (c *SystemdClient) LogReplayTree(ctx context.Context, repo, name, version string) (_ <-chan systemd.JournalEntry, err error) {
	q := fmt.Sprintf("systemd/logs/tree?repo=%s&name=%s&version=%s",
		url.QueryEscape(repo), url.QueryEscape(name), url.QueryEscape(version))
	return c.streamLogReplay(ctx, q, "systemd/logs/tree")
}

// LogTailTree returns a page of journal entries drawn from every unit in a
// package's dependency tree.
func (c *SystemdClient) LogTailTree(ctx context.Context, repo, name, version string, p systemd.LogTailParams) (systemd.LogTailResult, error) {
	q := fmt.Sprintf("systemd/logs/tree/tail?repo=%s&name=%s&version=%s&lines=%d",
		url.QueryEscape(repo), url.QueryEscape(name), url.QueryEscape(version), p.Lines)
	return c.fetchLogTail(ctx, appendLogTailQuery(q, p), "systemd/logs/tree/tail")
}

// appendLogTailQuery attaches the shared filter and pagination params onto
// the caller's base query string. The base string must already contain the
// unit selector (either `unit=` for the single-unit endpoint or
// `repo=&name=&version=` for the tree endpoint) plus `lines=`.
func appendLogTailQuery(q string, p systemd.LogTailParams) string {
	if p.BeforeCursor != "" {
		q = fmt.Sprintf("%s&before=%s", q, url.QueryEscape(p.BeforeCursor))
	}
	if p.AfterCursor != "" {
		q = fmt.Sprintf("%s&after=%s", q, url.QueryEscape(p.AfterCursor))
	}
	if p.Grep != "" {
		q = fmt.Sprintf("%s&grep=%s", q, url.QueryEscape(p.Grep))
	}
	if !p.Since.IsZero() {
		q = fmt.Sprintf("%s&since=%d", q, p.Since.Unix())
	}
	if !p.Until.IsZero() {
		q = fmt.Sprintf("%s&until=%d", q, p.Until.Unix())
	}
	if p.Priority > 0 {
		q = fmt.Sprintf("%s&priority=%d", q, p.Priority)
	}
	return q
}

// fetchLogTail is the shared transport for the tail endpoints: it issues
// the GET, decodes the JSON body, and surfaces problem-detail errors on
// non-200 responses.
func (c *SystemdClient) fetchLogTail(ctx context.Context, q, pathForErr string) (_ systemd.LogTailResult, err error) {
	resp, err := c.getClient(ctx, q)
	if err != nil {
		return systemd.LogTailResult{}, fmt.Errorf("%w: LogTail: %w", ErrHTTPRequest, err)
	}
	defer func() {
		err = errors.Join(err, resp.Body.Close())
	}()

	if resp.StatusCode != http.StatusOK {
		return systemd.LogTailResult{}, readProblemDetail(resp, "GET", pathForErr)
	}

	var result systemd.LogTailResult
	return result, json.NewDecoder(resp.Body).Decode(&result)
}

// streamLogReplay is the shared SSE transport for the single-unit and
// tree replay endpoints.
func (c *SystemdClient) streamLogReplay(ctx context.Context, q, pathForErr string) (_ <-chan systemd.JournalEntry, err error) {
	resp, err := c.getClient(ctx, q) //nolint:bodyclose // closed in goroutine below
	if err != nil {
		if resp != nil {
			err = errors.Join(err, resp.Body.Close())
		}
		return nil, fmt.Errorf("%w: LogReplay: %w", ErrHTTPRequest, err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, readProblemDetailAndClose(resp, "GET", pathForErr)
	}

	ch := make(chan systemd.JournalEntry)
	go func() {
		defer close(ch)
		defer func() {
			err = errors.Join(err, resp.Body.Close())
		}()

		scanner := bufio.NewScanner(resp.Body)
		for scanner.Scan() {
			line := scanner.Text()
			if !strings.HasPrefix(line, "data: ") {
				continue
			}

			var entry systemd.JournalEntry
			if err := json.NewDecoder(strings.NewReader(strings.TrimPrefix(line, "data: "))).Decode(&entry); err != nil {
				return
			}
			select {
			case ch <- entry:
			case <-ctx.Done():
				return
			}
		}
	}()

	return ch, nil
}
