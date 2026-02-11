package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

var suits = map[byte]string{
	'c': "clubs",
	'd': "diamonds",
	's': "spades",
	'h': "hearts",
}

var ranks = map[string]string{
	"2": "2", "3": "3", "4": "4", "5": "5",
	"6": "6", "7": "7", "8": "8", "9": "9", "10": "10",
	"j": "jack", "q": "queen", "k": "king", "a": "ace",
}

// shorthandToFilename converts e.g. "2c" -> "2_of_clubs.png", "ad" -> "ace_of_diamonds.png"
func shorthandToFilename(s string) (string, error) {
	s = strings.TrimSpace(strings.ToLower(s))
	if len(s) < 2 {
		return "", fmt.Errorf("invalid card shorthand: %q", s)
	}

	suitChar := s[len(s)-1]
	rankStr := s[:len(s)-1]

	suit, ok := suits[suitChar]
	if !ok {
		return "", fmt.Errorf("unknown suit %q in %q", string(suitChar), s)
	}

	rank, ok := ranks[rankStr]
	if !ok {
		return "", fmt.Errorf("unknown rank %q in %q", rankStr, s)
	}

	return fmt.Sprintf("%s_of_%s.png", rank, suit), nil
}

// readDeck reads card shorthands from a JSON array file.
func readDeck(path string) ([]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cards []string
	if err := json.Unmarshal(data, &cards); err != nil {
		return nil, fmt.Errorf("parsing deck %s: %w", path, err)
	}
	return cards, nil
}
