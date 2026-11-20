package main

import (
	"reflect"
	"testing"
	"time"
)

func TestDayKeyBoundary(t *testing.T) {
	cases := []struct {
		in   time.Time
		want string
	}{
		{time.Date(2026, 11, 18, 7, 59, 59, 0, time.UTC), "2026-11-17"},
		{time.Date(2026, 11, 18, 8, 0, 0, 0, time.UTC), "2026-11-18"},
		{time.Date(2026, 11, 18, 0, 0, 0, 0, time.UTC), "2026-11-17"},
		{time.Date(2026, 11, 18, 23, 30, 0, 0, time.UTC), "2026-11-18"},
		// Non-UTC input is converted first: 15:59 CST = 07:59 UTC.
		{time.Date(2026, 11, 18, 15, 59, 0, 0, time.FixedZone("CST", 8*3600)), "2026-11-17"},
		{time.Date(2026, 11, 18, 16, 0, 0, 0, time.FixedZone("CST", 8*3600)), "2026-11-18"},
	}
	for _, c := range cases {
		if got := dayKey(c.in); got != c.want {
			t.Errorf("dayKey(%v) = %s, want %s", c.in, got, c.want)
		}
	}
}

func TestLevelAndQuota(t *testing.T) {
	cases := []struct {
		days      int
		wantLevel int
		wantQuota int
	}{
		{0, 1, 24},
		{1, 1, 24},
		{99, 1, 24},
		{100, 2, 48},
		{199, 2, 48},
		{200, 3, 60},
		{300, 4, 66},
		{400, 5, 69},
		{500, 6, 71},
		{600, 7, 72},
		{700, 8, 73},
		{900, 10, 75},
		{3600, 37, 102}, // ~ten years of daily attendance
	}
	for _, c := range cases {
		if got := levelForDays(c.days); got != c.wantLevel {
			t.Errorf("levelForDays(%d) = %d, want %d", c.days, got, c.wantLevel)
		}
		if got := dailyQuota(c.days); got != c.wantQuota {
			t.Errorf("dailyQuota(%d) = %d, want %d", c.days, got, c.wantQuota)
		}
	}
}

func TestNameValidation(t *testing.T) {
	valid := []string{"诗", "乌青UQn", "Wren", "Echo7", "海盐", "小满", "abcdefghijklmnop", "诗诗诗诗诗诗诗诗"}
	for _, n := range valid {
		if msg := validateName(n); msg != "" {
			t.Errorf("validateName(%q) = %q, want valid", n, msg)
		}
	}
	invalid := map[string]string{
		"":                  "empty",
		"a":                 "width 1, too short",
		"hello world":       "space not allowed",
		"abc!":              "symbol not allowed",
		"a😀":                "emoji not allowed",
		"诗诗诗诗诗诗诗诗诗":         "width 18, too long",
		"abcdefghijklmnopq": "width 17, too long",
		"name_with":         "underscore not allowed",
		"Admin":             "blocklisted",
		"xFUCKx":            "blocklisted, case-insensitive",
	}
	for n, why := range invalid {
		if msg := validateName(n); msg == "" {
			t.Errorf("validateName(%q) = valid, want invalid (%s)", n, why)
		}
	}
	if w, ok := nameWidth("乌青UQn"); !ok || w != 7 {
		t.Errorf("nameWidth(乌青UQn) = %d,%v, want 7,true", w, ok)
	}
	if nameKeyOf("Alice") != nameKeyOf("alice") {
		t.Error("name keys must fold case")
	}
}

func TestWindowK(t *testing.T) {
	cases := map[int]int{0: 0, 1: 1, 2: 2, 3: 3, 10: 3, 20: 3, 40: 6, 93: 14, 100: 15, 133: 20, 1000: 20}
	for n, want := range cases {
		if got := windowK(n); got != want {
			t.Errorf("windowK(%d) = %d, want %d", n, got, want)
		}
	}
}

func TestSampleWindowDeterministic(t *testing.T) {
	ids := make([]int64, 93)
	for i := range ids {
		ids[i] = int64(i + 1)
	}
	a := sampleWindow(ids, "u:7", "2026-11-18")
	b := sampleWindow(ids, "u:7", "2026-11-18")
	if !reflect.DeepEqual(a, b) {
		t.Error("same viewer/day must give the same sample")
	}
	if len(a) != 14 {
		t.Errorf("sample size = %d, want 14", len(a))
	}
	seen := map[int64]bool{}
	for _, id := range a {
		if seen[id] {
			t.Errorf("duplicate id %d in sample", id)
		}
		seen[id] = true
		if id < 1 || id > 93 {
			t.Errorf("sample id %d not in roster", id)
		}
	}
	c := sampleWindow(ids, "u:8", "2026-11-18")
	if reflect.DeepEqual(a, c) {
		t.Error("different viewers should (almost surely) get different samples")
	}
	d := sampleWindow(ids, "u:7", "2026-11-19")
	if reflect.DeepEqual(a, d) {
		t.Error("different days should (almost surely) give different samples")
	}
	if got := sampleWindow(nil, "u:7", "2026-11-18"); len(got) != 0 {
		t.Errorf("empty roster sample = %v, want empty", got)
	}
	small := sampleWindow([]int64{5, 9}, "a:xyz", "2026-11-18")
	if len(small) != 2 {
		t.Errorf("tiny roster sample = %v, want both members", small)
	}
}
