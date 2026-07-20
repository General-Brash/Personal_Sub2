package service

import (
	"math/rand"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestContentModerationKeywordMatcherBoundaryRules(t *testing.T) {
	tests := []struct {
		name        string
		text        string
		keywords    []string
		wantKeyword string
		wantHit     bool
	}{
		{name: "miss", text: "clean prompt", keywords: []string{"blocked", "secret"}},
		{name: "case insensitive with punctuation", text: "say HI!", keywords: []string{"hi"}, wantKeyword: "hi", wantHit: true},
		{name: "ascii prefix does not match", text: "review history", keywords: []string{"hi"}},
		{name: "ascii infix does not match", text: "this is clean", keywords: []string{"hi"}},
		{name: "underscore is a word character", text: "_hi hi_2", keywords: []string{"hi"}},
		{name: "configured order wins", text: "early appears before later", keywords: []string{"later", "early"}, wantKeyword: "later", wantHit: true},
		{name: "invalid earlier keyword falls through", text: "history and abc", keywords: []string{"hi", "abc"}, wantKeyword: "abc", wantHit: true},
		{name: "invalid suffix falls through at same endpoint", text: "thishi", keywords: []string{"hi", "thishi"}, wantKeyword: "thishi", wantHit: true},
		{name: "overlap requires boundaries", text: "abc", keywords: []string{"bc", "abc"}, wantKeyword: "abc", wantHit: true},
		{name: "multiword phrase", text: "a BAD WORD!", keywords: []string{"bad word"}, wantKeyword: "bad word", wantHit: true},
		{name: "multiword phrase rejects suffix", text: "bad wording", keywords: []string{"bad word"}},
		{name: "multiword phrase rejects prefix", text: "notbad word", keywords: []string{"bad word"}},
		{name: "chinese remains substring based", text: "这里包含敏感词和世界", keywords: []string{"世界", "敏感"}, wantKeyword: "世界", wantHit: true},
		{name: "ascii boundary beside chinese", text: "hi中文", keywords: []string{"hi"}, wantKeyword: "hi", wantHit: true},
		{name: "duplicates", text: "duplicate", keywords: []string{"duplicate", "DUPLICATE"}, wantKeyword: "duplicate", wantHit: true},
		{name: "empty entries", text: "blocked", keywords: []string{"", "blocked"}, wantKeyword: "blocked", wantHit: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotKeyword, gotHit := newContentModerationKeywordMatcher(tt.keywords).Match(tt.text)
			require.Equal(t, tt.wantHit, gotHit)
			require.Equal(t, tt.wantKeyword, gotKeyword)
		})
	}
}

func TestContentModerationKeywordMatcherRandomizedParity(t *testing.T) {
	rng := rand.New(rand.NewSource(20260714))
	const alphabet = "abcXYZ _-."
	for iteration := 0; iteration < 1000; iteration++ {
		keywords := make([]string, 1+rng.Intn(30))
		for index := range keywords {
			length := 1 + rng.Intn(8)
			var value strings.Builder
			for range length {
				_ = value.WriteByte(alphabet[rng.Intn(len(alphabet))])
			}
			keywords[index] = value.String()
		}
		var text strings.Builder
		for range 20 + rng.Intn(100) {
			_ = text.WriteByte(alphabet[rng.Intn(len(alphabet))])
		}

		wantKeyword, wantHit := matchBlockedKeyword(text.String(), keywords)
		gotKeyword, gotHit := newContentModerationKeywordMatcher(keywords).Match(text.String())
		require.Equal(t, wantHit, gotHit, "iteration %d", iteration)
		require.Equal(t, wantKeyword, gotKeyword, "iteration %d", iteration)
	}
}
