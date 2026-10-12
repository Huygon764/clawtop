package main

import "testing"

func TestCallsFromEventsGroupsRuns(t *testing.T) {
	events := []eventRow{
		{Seq: 1, Raw: `{
			"type":"message",
			"message":{"role":"user","content":"hello there","timestamp":1791593603088}
		}`},
		{Seq: 2, Raw: `{
			"type":"message",
			"message":{
				"role":"assistant",
				"content":[{"type":"text","text":"ok"},{"type":"toolCall","name":"exec"}],
				"stopReason":"toolUse",
				"usage":{"input":906,"output":78},
				"timestamp":1791593604000,
				"__openclaw":{"runId":"r1"}
			}
		}`},
		{Seq: 3, Raw: `{
			"type":"message",
			"message":{"role":"toolResult","content":[{"type":"text","text":"x"}],"timestamp":1791593605000}
		}`},
		{Seq: 4, Raw: `{
			"type":"message",
			"message":{
				"role":"assistant",
				"content":[{"type":"toolCall","name":"exec"},{"type":"text","text":"again"}],
				"stopReason":"toolUse",
				"usage":{"input":1024,"output":99},
				"timestamp":1791593606000,
				"__openclaw":{"runId":"r1"}
			}
		}`},
		{Seq: 5, Raw: `{
			"type":"message",
			"message":{
				"role":"assistant",
				"content":[{"type":"text","text":"done"}],
				"stopReason":"stop",
				"usage":{"input":36403,"output":3},
				"timestamp":1791593607000,
				"__openclaw":{"runId":"r1"}
			}
		}`},
	}
	calls := callsFromEvents(events)
	if len(calls) != 3 {
		t.Fatalf("calls = %d, want 3", len(calls))
	}
	if calls[0].Label != "hello there" || calls[0].Tool || calls[0].Input != 906 || calls[0].Output != 78 {
		t.Fatalf("first call = %+v", calls[0])
	}
	if calls[1].Label != "exec" || !calls[1].Tool || calls[1].Input != 1024 || calls[1].Output != 99 {
		t.Fatalf("second call = %+v", calls[1])
	}
	if calls[2].Label != "" || !calls[2].Tool || calls[2].Output != 3 {
		t.Fatalf("third call = %+v", calls[2])
	}
	if formatClock(calls[0].At) == "" {
		t.Fatal("missing clock")
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
	if latest.DisplayKey() == "" || len(latest.Calls) == 0 {
		t.Fatalf("latest session empty: %+v", latest.DisplayKey())
	}
	var waste int
	for _, call := range latest.Calls {
		if call.Input < 0 || call.Output < 0 {
			t.Fatalf("negative usage: %+v", call)
		}
		if call.Output <= 3 {
			waste++
		}
	}
	if waste == 0 {
		t.Fatal("expected highlighted calls")
	}
}
