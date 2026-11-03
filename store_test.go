package main

import (
	"strings"
	"testing"
)

func TestRequestsFromEvents(t *testing.T) {
	events := []eventRow{
		{Seq: 1, Raw: `{
			"type":"message",
			"message":{"role":"user","content":"hello there","timestamp":1791593603088}
		}`},
		{Seq: 2, Raw: `{
			"type":"message",
			"message":{
				"role":"assistant",
				"content":[{"type":"toolCall","name":"tool_call","arguments":{"id":"web_search","args":{"query":"hello"}}}],
				"stopReason":"toolUse",
				"usage":{"input":906,"output":78},
				"timestamp":1791593604000
			}
		}`},
		{Seq: 3, Raw: `{
			"type":"message",
			"message":{"role":"toolResult","content":[{"type":"text","text":"{\"status\":\"error\",\"error\":\"web_search is disabled or no provider is available.\"}"}],"timestamp":1791593605000}
		}`},
		{Seq: 4, Raw: `{
			"type":"message",
			"message":{
				"role":"assistant",
				"content":[{"type":"toolCall","name":"web_fetch","arguments":{"url":"https://vnexpress.net/rss"}}],
				"stopReason":"toolUse",
				"usage":{"input":1024,"output":40},
				"timestamp":1791593606000
			}
		}`},
		{Seq: 5, Raw: `{
			"type":"message",
			"message":{
				"role":"assistant",
				"content":[{"type":"text","text":"five headlines"}],
				"stopReason":"stop",
				"usage":{"input":36403,"output":20},
				"timestamp":1791593607000
			}
		}`},
		{Seq: 6, Raw: `{
			"type":"message",
			"message":{"role":"user","content":"[OpenClaw heartbeat poll]","timestamp":1791593610000}
		}`},
		{Seq: 7, Raw: `{
			"type":"message",
			"message":{
				"role":"assistant",
				"content":[{"type":"text","text":"NO_REPLY"}],
				"stopReason":"stop",
				"usage":{"input":36000,"output":2},
				"timestamp":1791593611000
			}
		}`},
	}
	reqs := requestsFromEvents(events)
	if len(reqs) != 2 {
		t.Fatalf("requests = %d, want 2", len(reqs))
	}
	first := reqs[0]
	if first.Heartbeat || !first.Failed || first.User != "hello there" || first.Reply != "five headlines" {
		t.Fatalf("first request = %+v", first)
	}
	if first.Input != 906+1024+36403 || first.Output != 78+40+20 {
		t.Fatalf("totals in=%d out=%d", first.Input, first.Output)
	}
	if len(first.Steps) != 4 {
		t.Fatalf("steps = %d, want 4", len(first.Steps))
	}
	if first.Steps[0].Label != "hello there" || first.Steps[0].Waste {
		t.Fatalf("first step = %+v", first.Steps[0])
	}
	if !first.Steps[1].IsError || !strings.Contains(first.Steps[1].Label, "no provider") {
		t.Fatalf("error step = %+v", first.Steps[1])
	}
	if first.Steps[2].Label != "web_fetch  url=https://vnexpress.net/rss" {
		t.Fatalf("fetch step = %+v", first.Steps[2])
	}
	if first.Steps[3].Label != "five headlines" {
		t.Fatalf("reply step = %+v", first.Steps[3])
	}
	second := reqs[1]
	if !second.Heartbeat || second.Reply != "NO_REPLY" || !second.Steps[0].Waste {
		t.Fatalf("heartbeat = %+v", second)
	}
	if second.Steps[0].Label != "[OpenClaw heartbeat poll]" {
		t.Fatalf("single-call label = %q", second.Steps[0].Label)
	}
}

func TestLoadOpenClaw(t *testing.T) {
	sessions, err := Load()
	if err != nil && len(sessions) == 0 {
		t.Fatal(err)
	}
	if len(sessions) == 0 {
		t.Fatal("no sessions")
	}
	latest := sessions[len(sessions)-1]
	if latest.DisplayKey() == "" || len(latest.Requests) == 0 {
		t.Fatalf("latest session empty: %s", latest.DisplayKey())
	}
	if latest.Waste() < 0 || latest.Input() <= 0 {
		t.Fatalf("bad totals in=%d waste=%d", latest.Input(), latest.Waste())
	}
}
