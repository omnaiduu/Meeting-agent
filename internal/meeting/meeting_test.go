package meeting

import (
	"errors"
	"testing"
)

func TestParseURL(t *testing.T) {
	s := &Service{cfg: Config{AllowedHosts: []string{"meet.google.com", "localhost:9090"}}}
	ok := map[string]string{
		"https://meet.google.com/abc-defg-hij":            "abc-defg-hij",
		"meet.google.com/abc-defg-hij":                    "abc-defg-hij", // scheme added
		"https://meet.google.com/ABC-DEFG-HIJ?authuser=1": "abc-defg-hij",
		"  https://meet.google.com/abc-defg-hij#x  ":      "abc-defg-hij",
		"http://localhost:9090/anything":                  "anything", // fake Meet for demos
	}
	for in, code := range ok {
		_, got, err := s.ParseURL(in)
		if err != nil || got != code {
			t.Errorf("%q: got %q, %v", in, got, err)
		}
	}
	bad := []string{
		"", "not a url", "https://zoom.us/j/123", "https://meet.google.com/", "https://meet.google.com/lookup/xyz",
		"http://meet.google.com/abc-defg-hij", // must be https
		"https://evil.com/abc-defg-hij", "https://meet.google.com.evil.com/abc-defg-hij",
	}
	for _, in := range bad {
		if _, _, err := s.ParseURL(in); !errors.Is(err, ErrInvalidURL) {
			t.Errorf("%q: want ErrInvalidURL, got %v", in, err)
		}
	}
}
