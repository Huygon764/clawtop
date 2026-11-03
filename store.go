package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/klauspost/compress/zstd"
	_ "modernc.org/sqlite"
)

const userLabelRunes = 60

type Step struct {
	Seq     int
	At      time.Time
	Input   int
	Output  int
	Label   string
	Tool    string
	IsError bool
	Final   bool
	Waste   bool
}

type Request struct {
	Seq       int
	At        time.Time
	User      string
	Reply     string
	Input     int
	Output    int
	Failed    bool
	Heartbeat bool
	Steps     []Step
}

type Session struct {
	Key      string
	ID       string
	Latest   time.Time
	Requests []Request
}

func (s Session) DisplayKey() string {
	const prefix = "agent:"
	if len(s.Key) >= len(prefix) && s.Key[:len(prefix)] == prefix {
		return s.Key[len(prefix):]
	}
	if s.Key != "" {
		return s.Key
	}
	if len(s.ID) > 8 {
		return s.ID[:8]
	}
	return s.ID
}

func (s Session) Waste() int {
	n := 0
	for _, req := range s.Requests {
		for _, step := range req.Steps {
			if step.Waste {
				n += step.Input
			}
		}
	}
	return n
}

func (s Session) Input() int {
	n := 0
	for _, req := range s.Requests {
		n += req.Input
	}
	return n
}

func (s Session) visible(showHeartbeat bool) []Request {
	if showHeartbeat {
		return s.Requests
	}
	out := make([]Request, 0, len(s.Requests))
	for _, req := range s.Requests {
		if !req.Heartbeat {
			out = append(out, req)
		}
	}
	return out
}

func transcriptPaths() ([]string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	pattern := filepath.Join(home, ".openclaw", "agents", "*", "agent", "openclaw-agent.sqlite")
	return filepath.Glob(pattern)
}

func Load() ([]Session, error) {
	paths, err := transcriptPaths()
	if err != nil {
		return nil, err
	}
	if len(paths) == 0 {
		return nil, fmt.Errorf("no transcript database under ~/.openclaw/agents")
	}

	var sessions []Session
	var firstErr error
	for _, path := range paths {
		got, err := loadDB(path)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		sessions = append(sessions, got...)
	}
	sort.Slice(sessions, func(i, j int) bool {
		if sessions[i].Latest.Equal(sessions[j].Latest) {
			return sessions[i].Key < sessions[j].Key
		}
		return sessions[i].Latest.Before(sessions[j].Latest)
	})
	if len(sessions) == 0 && firstErr != nil {
		return nil, firstErr
	}
	return sessions, firstErr
}

func loadDB(path string) ([]Session, error) {
	dsn := sqliteReadOnlyDSN(path)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	db.SetMaxOpenConns(1)

	keys := map[string]string{}
	rows, err := db.Query(`SELECT session_key, current_session_id FROM session_nodes`)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", filepath.Base(path), err)
	}
	for rows.Next() {
		var key string
		var id sql.NullString
		if err := rows.Scan(&key, &id); err != nil {
			rows.Close()
			return nil, err
		}
		if id.Valid && id.String != "" {
			keys[id.String] = key
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()

	idRows, err := db.Query(`SELECT DISTINCT session_id FROM transcript_events`)
	if err != nil {
		return nil, err
	}
	var ids []string
	for idRows.Next() {
		var id string
		if err := idRows.Scan(&id); err != nil {
			idRows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	if err := idRows.Err(); err != nil {
		idRows.Close()
		return nil, err
	}
	idRows.Close()

	var sessions []Session
	for _, id := range ids {
		session, err := loadSession(db, id, keys[id])
		if err != nil {
			return nil, err
		}
		if session.ID == "" {
			continue
		}
		sessions = append(sessions, session)
	}
	return sessions, nil
}

func sqliteReadOnlyDSN(path string) string {
	u := url.URL{
		Scheme:   "file",
		Path:     path,
		RawQuery: "mode=ro&_pragma=busy_timeout(2000)",
	}
	return u.String()
}

type eventRow struct {
	Seq       int
	Raw       string
	CreatedAt int64
}

var zdec = func() *zstd.Decoder {
	dec, err := zstd.NewReader(nil)
	if err != nil {
		panic(err)
	}
	return dec
}()

func loadSession(db *sql.DB, sessionID, key string) (Session, error) {
	rows, err := db.Query(`
		SELECT seq, event_json, event_zstd, created_at
		FROM transcript_events
		WHERE session_id = ?
		ORDER BY seq`, sessionID)
	if err != nil {
		return Session{}, err
	}
	defer rows.Close()

	var events []eventRow
	for rows.Next() {
		var row eventRow
		var raw sql.NullString
		var blob []byte
		var created sql.NullInt64
		if err := rows.Scan(&row.Seq, &raw, &blob, &created); err != nil {
			return Session{}, err
		}
		if raw.Valid && raw.String != "" {
			row.Raw = raw.String
		} else if len(blob) > 0 {
			plain, err := zdec.DecodeAll(blob, nil)
			if err != nil {
				continue
			}
			row.Raw = string(plain)
		} else {
			continue
		}
		if created.Valid {
			row.CreatedAt = created.Int64
		}
		events = append(events, row)
	}
	if err := rows.Err(); err != nil {
		return Session{}, err
	}
	reqs := requestsFromEvents(events)
	if len(reqs) == 0 {
		return Session{}, nil
	}
	var latest time.Time
	for _, req := range reqs {
		if req.At.After(latest) {
			latest = req.At
		}
		for _, step := range req.Steps {
			if step.At.After(latest) {
				latest = step.At
			}
		}
	}
	return Session{Key: key, ID: sessionID, Latest: latest, Requests: reqs}, nil
}

func requestsFromEvents(events []eventRow) []Request {
	var reqs []Request
	var cur *Request

	flush := func() {
		if cur == nil {
			return
		}
		finishRequest(cur)
		if cur.User != "" || len(cur.Steps) > 0 {
			reqs = append(reqs, *cur)
		}
		cur = nil
	}

	for _, event := range events {
		msg, ok := decodeMessage(event.Raw)
		if !ok {
			continue
		}
		switch msg.Role {
		case "user":
			flush()
			text := cleanText(textFromContent(msg.Content))
			cur = &Request{
				Seq:       event.Seq,
				At:        msg.when(event.CreatedAt),
				User:      text,
				Heartbeat: isHeartbeat(text),
			}
		case "assistant":
			if cur == nil {
				cur = &Request{Seq: event.Seq, At: msg.when(event.CreatedAt)}
			}
			step := Step{
				Seq:    event.Seq,
				At:     msg.when(event.CreatedAt),
				Input:  msg.Input,
				Output: msg.Output,
				Tool:   toolLabel(msg.Content),
				Final:  msg.StopReason == "stop",
			}
			cur.Steps = append(cur.Steps, step)
			if step.Final {
				cur.Reply = cleanText(textFromContent(msg.Content))
			}
		case "toolResult":
			if cur == nil {
				continue
			}
			errText := toolError(msg.Content)
			if errText == "" {
				continue
			}
			cur.Failed = true
			cur.Steps = append(cur.Steps, Step{
				Seq:     event.Seq,
				At:      msg.when(event.CreatedAt),
				Label:   clip(errText, 160),
				IsError: true,
			})
		}
	}
	flush()
	return reqs
}

func finishRequest(req *Request) {
	var calls []int
	for i, step := range req.Steps {
		if step.IsError {
			continue
		}
		calls = append(calls, i)
		req.Input += step.Input
		req.Output += step.Output
		if step.Output <= 3 {
			req.Steps[i].Waste = true
		}
	}
	if len(calls) == 0 {
		return
	}
	req.Steps[calls[0]].Label = clip(req.User, userLabelRunes)
	for _, i := range calls[1:] {
		req.Steps[i].Label = req.Steps[i].Tool
	}
	last := calls[len(calls)-1]
	if len(calls) > 1 && req.Steps[last].Final && req.Reply != "" {
		req.Steps[last].Label = clip(req.Reply, userLabelRunes)
	}
	if req.At.IsZero() {
		req.At = req.Steps[calls[0]].At
	}
}

func isHeartbeat(user string) bool {
	return strings.Contains(strings.ToLower(user), "openclaw heartbeat")
}

type decodedMessage struct {
	Role       string
	Content    json.RawMessage
	StopReason string
	Input      int
	Output     int
	Stamp      time.Time
}

func (m decodedMessage) when(createdAt int64) time.Time {
	if !m.Stamp.IsZero() {
		return m.Stamp
	}
	return unixFlex(createdAt)
}

func decodeMessage(raw string) (decodedMessage, bool) {
	var ev struct {
		Message *struct {
			Role       string          `json:"role"`
			Content    json.RawMessage `json:"content"`
			StopReason string          `json:"stopReason"`
			Usage      *struct {
				Input  flexInt `json:"input"`
				Output flexInt `json:"output"`
			} `json:"usage"`
			Timestamp json.RawMessage `json:"timestamp"`
		} `json:"message"`
	}
	if err := json.Unmarshal([]byte(raw), &ev); err != nil || ev.Message == nil {
		return decodedMessage{}, false
	}
	msg := ev.Message
	out := decodedMessage{
		Role:       msg.Role,
		Content:    msg.Content,
		StopReason: msg.StopReason,
		Stamp:      parseStamp(msg.Timestamp),
	}
	if msg.Usage != nil {
		out.Input = int(msg.Usage.Input)
		out.Output = int(msg.Usage.Output)
	}
	return out, true
}

func textFromContent(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return text
	}
	var parts []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if json.Unmarshal(raw, &parts) != nil {
		return ""
	}
	var b string
	for _, part := range parts {
		if part.Type == "text" || part.Type == "" {
			b += part.Text + " "
		}
	}
	return b
}

func toolLabel(raw json.RawMessage) string {
	var parts []struct {
		Type      string          `json:"type"`
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if json.Unmarshal(raw, &parts) != nil {
		return ""
	}
	for _, part := range parts {
		if part.Type != "toolCall" || part.Name == "" {
			continue
		}
		name := part.Name
		args := part.Arguments
		if name == "tool_call" || name == "tool_describe" {
			var inner struct {
				ID   string          `json:"id"`
				Args json.RawMessage `json:"args"`
			}
			if json.Unmarshal(args, &inner) == nil && inner.ID != "" {
				name = inner.ID
				if len(inner.Args) > 0 {
					args = inner.Args
				}
			}
		}
		if arg := pickArg(args); arg != "" {
			return name + "  " + arg
		}
		return name
	}
	return ""
}

func pickArg(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var fields map[string]any
	if json.Unmarshal(raw, &fields) != nil {
		return ""
	}
	for _, key := range []string{"query", "url", "path"} {
		value, ok := fields[key].(string)
		if !ok || value == "" {
			continue
		}
		value = cleanText(value)
		if key == "query" {
			return key + `="` + clip(value, userLabelRunes) + `"`
		}
		return key + "=" + clip(value, 80)
	}
	return ""
}

func toolError(raw json.RawMessage) string {
	text := strings.TrimSpace(textFromContent(raw))
	if text == "" {
		return ""
	}
	var wrap struct {
		Status string          `json:"status"`
		Error  json.RawMessage `json:"error"`
	}
	if json.Unmarshal([]byte(text), &wrap) != nil || wrap.Status != "error" {
		return ""
	}
	var msg string
	if json.Unmarshal(wrap.Error, &msg) != nil {
		msg = string(wrap.Error)
	}
	return cleanText(unwrapExternal(msg))
}

func unwrapExternal(s string) string {
	const marker = "---\n"
	const end = "<<<END_EXTERNAL_UNTRUSTED_CONTENT"
	if i := strings.LastIndex(s, marker); i >= 0 {
		s = s[i+len(marker):]
	}
	if j := strings.Index(s, end); j >= 0 {
		s = s[:j]
	}
	return strings.TrimSpace(s)
}

type flexInt int

func (n *flexInt) UnmarshalJSON(b []byte) error {
	if string(b) == "null" {
		return nil
	}
	var i int
	if err := json.Unmarshal(b, &i); err == nil {
		*n = flexInt(i)
		return nil
	}
	var f float64
	if err := json.Unmarshal(b, &f); err == nil {
		*n = flexInt(f)
		return nil
	}
	return fmt.Errorf("not a number: %s", b)
}

func parseStamp(raw json.RawMessage) time.Time {
	if len(raw) == 0 || string(raw) == "null" {
		return time.Time{}
	}
	var n int64
	if json.Unmarshal(raw, &n) == nil {
		return unixFlex(n)
	}
	var f float64
	if json.Unmarshal(raw, &f) == nil {
		return unixFlex(int64(f))
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		if t, err := time.Parse(time.RFC3339, s); err == nil {
			return t.Local()
		}
		if n, err := strconv.ParseInt(s, 10, 64); err == nil {
			return unixFlex(n)
		}
	}
	return time.Time{}
}

func unixFlex(n int64) time.Time {
	if n <= 0 {
		return time.Time{}
	}
	if n > 1_000_000_000_000 {
		return time.UnixMilli(n).Local()
	}
	return time.Unix(n, 0).Local()
}
