package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"
	"unicode"
)

// ---------------------------------------------------------------------------
// Day boundary: one global Space day for everyone; it flips at 08:00 UTC.
// dayKey is the calendar date of (nowUTC - 8h).
// ---------------------------------------------------------------------------

func dayKey(t time.Time) string {
	return t.UTC().Add(-8 * time.Hour).Format("2006-01-02")
}

// ---------------------------------------------------------------------------
// Levels & daily quota. Level comes from cumulative attendance days:
// level = 1 + floor(days/100). Quota table (decided):
// Lv1=24, Lv2=48, Lv3=60, Lv4=66, Lv5=69, Lv6=71, Lv7=72, Lv8+ = +1 per level.
// ---------------------------------------------------------------------------

func levelForDays(days int) int {
	if days < 0 {
		days = 0
	}
	return 1 + days/100
}

func quotaForLevel(level int) int {
	switch {
	case level <= 1:
		return 24
	case level == 2:
		return 48
	case level == 3:
		return 60
	case level == 4:
		return 66
	case level == 5:
		return 69
	case level == 6:
		return 71
	case level == 7:
		return 72
	default:
		return 72 + (level - 7)
	}
}

func dailyQuota(days int) int { return quotaForLevel(levelForDays(days)) }

// ---------------------------------------------------------------------------
// Names: only Han characters, letters and digits (whitelist — no spaces,
// punctuation, symbols or emoji). Width: Han = 2 units, letter/digit = 1.
// Total width must be 2..16. Uniqueness is case-insensitive; display keeps
// the original case.
// ---------------------------------------------------------------------------

func nameKeyOf(name string) string { return strings.ToLower(name) }

// nameWidth returns the display width and whether every rune is allowed.
func nameWidth(name string) (int, bool) {
	w := 0
	for _, r := range name {
		switch {
		case unicode.Is(unicode.Han, r):
			w += 2
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			w += 1
		default:
			return 0, false
		}
	}
	return w, true
}

// Small blocklist: profanity + impersonation of the service itself.
// Matched case-insensitively as a substring of the folded name.
var nameBlocklist = []string{
	"fuck", "shit", "bitch", "cunt", "nigger", "nigga", "faggot", "whore",
	"porn", "rape", "nazi", "hitler",
	"admin", "official", "seeyouhere", "support", "moderator", "system", "staff",
}

// validateName returns "" when the name is acceptable, else a user-facing reason.
func validateName(name string) string {
	if name == "" {
		return "Please choose a name."
	}
	w, ok := nameWidth(name)
	if !ok {
		return "Only letters, digits and Chinese characters — no spaces, symbols or emoji."
	}
	if w < 2 {
		return "That name is too short."
	}
	if w > 16 {
		return "That name is too long (max 16 units; a Chinese character counts as 2)."
	}
	key := nameKeyOf(name)
	for _, bad := range nameBlocklist {
		if strings.Contains(key, bad) {
			return "That name isn't allowed."
		}
	}
	return ""
}

// ---------------------------------------------------------------------------
// Window sampling: viewers who haven't checked in see only a sample of
// today's roster. K = round(15% of roster size), min 3, max 20 (and never
// more than the roster itself). The sample is deterministic per viewer per
// day: order by hash(viewerKey || dayKey || userID), take the first K.
// ---------------------------------------------------------------------------

func windowK(rosterSize int) int {
	if rosterSize <= 0 {
		return 0
	}
	k := int(math.Round(0.15 * float64(rosterSize)))
	if k < 3 {
		k = 3
	}
	if k > 20 {
		k = 20
	}
	if k > rosterSize {
		k = rosterSize
	}
	return k
}

func sampleWindow(ids []int64, viewerKey, day string) []int64 {
	k := windowK(len(ids))
	if k == 0 {
		return nil
	}
	type scored struct {
		id   int64
		hash string
	}
	sc := make([]scored, len(ids))
	for i, id := range ids {
		sum := sha256.Sum256([]byte(fmt.Sprintf("%s|%s|%d", viewerKey, day, id)))
		sc[i] = scored{id: id, hash: hex.EncodeToString(sum[:])}
	}
	sort.Slice(sc, func(i, j int) bool { return sc[i].hash < sc[j].hash })
	out := make([]int64, k)
	for i := 0; i < k; i++ {
		out[i] = sc[i].id
	}
	return out
}
