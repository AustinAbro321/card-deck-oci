package main

import (
	"bufio"
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

// readDeck reads card shorthands from a file, one per line, skipping blanks and # comments.
func readDeck(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var cards []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		cards = append(cards, line)
	}
	return cards, scanner.Err()
}
