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
	"time"

	_ "modernc.org/sqlite"
)

const userLabelRunes = 60

type Call struct {
	At     time.Time
	Input  int
	Output int
	Label  string
	Tool   bool
}

type Session struct {
	Key    string
	ID     string
	Latest time.Time
	Calls  []Call
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
		calls, latest, err := loadCalls(db, id)
		if err != nil {
			return nil, err
		}
		if len(calls) == 0 {
			continue
		}
		sessions = append(sessions, Session{
			Key:    keys[id],
			ID:     id,
			Latest: latest,
			Calls:  calls,
		})
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

func loadCalls(db *sql.DB, sessionID string) ([]Call, time.Time, error) {
	rows, err := db.Query(`
		SELECT seq, event_json, created_at
		FROM transcript_events
		WHERE session_id = ? AND event_json IS NOT NULL
		ORDER BY seq`, sessionID)
	if err != nil {
		return nil, time.Time{}, err
	}
	defer rows.Close()

	var events []eventRow
	for rows.Next() {
		var row eventRow
		var raw sql.NullString
		var created sql.NullInt64
		if err := rows.Scan(&row.Seq, &raw, &created); err != nil {
			return nil, time.Time{}, err
		}
		if !raw.Valid || raw.String == "" {
			continue
		}
		row.Raw = raw.String
		if created.Valid {
			row.CreatedAt = created.Int64
		}
		events = append(events, row)
	}
	if err := rows.Err(); err != nil {
		return nil, time.Time{}, err
	}
	calls := callsFromEvents(events)
	var latest time.Time
	for _, call := range calls {
		if call.At.After(latest) {
			latest = call.At
		}
	}
	return calls, latest, nil
}

func callsFromEvents(events []eventRow) []Call {
	var calls []Call
	var userText string
	var runID string
	inRun := false

	for _, event := range events {
		msg, ok := decodeMessage(event.Raw)
		if !ok {
			continue
		}
		switch msg.Role {
		case "user":
			userText = cleanText(textFromContent(msg.Content))
		case "assistant":
			at := msg.when(event.CreatedAt)
			label := ""
			tool := false
			if inRun && msg.RunID != "" && msg.RunID == runID {
				label = toolFromContent(msg.Content)
				if label == "" && msg.StopReason != "" && msg.StopReason != "stop" {
					label = msg.StopReason
				}
				tool = true
			} else {
				label = clip(userText, userLabelRunes)
				runID = msg.RunID
				inRun = true
			}
			calls = append(calls, Call{
				At:     at,
				Input:  msg.Input,
				Output: msg.Output,
				Label:  label,
				Tool:   tool,
			})
		}
	}
	return calls
}

type decodedMessage struct {
	Role       string
	Content    json.RawMessage
	StopReason string
	Input      int
	Output     int
	RunID      string
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
			OpenClaw  struct {
				RunID string `json:"runId"`
			} `json:"__openclaw"`
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
		RunID:      msg.OpenClaw.RunID,
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

func toolFromContent(raw json.RawMessage) string {
	var parts []struct {
		Type string `json:"type"`
		Name string `json:"name"`
	}
	if json.Unmarshal(raw, &parts) != nil {
		return ""
	}
	for _, part := range parts {
		if part.Type == "toolCall" && part.Name != "" {
			return part.Name
		}
	}
	return ""
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
