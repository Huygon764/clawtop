package main

import (
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

func formatTokens(n int) string {
	if n < 0 {
		return "-" + formatTokens(-n)
	}
	if n < 1000 {
		return strconv.Itoa(n)
	}
	if n < 1_000_000 {
		return strconv.FormatFloat(float64(n)/1000, 'f', 1, 64) + "k"
	}
	return strconv.FormatFloat(float64(n)/1_000_000, 'f', 1, 64) + "m"
}

func formatClock(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Local().Format("01-02 15:04")
}

func cleanText(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

func clip(s string, n int) string {
	if n <= 0 || s == "" {
		return ""
	}
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	if n == 1 {
		return "…"
	}
	runes := []rune(s)
	return string(runes[:n-1]) + "…"
}
