// Package weather fetches a one-day forecast from WeatherAPI.com.
package weather

import "time"

// Report is what the frame and the settings page need. It is a value, not a
// pointer, so a zero Report means "no weather to show".
type Report struct {
	City    string // location.name from the response
	Region  string // location.region, may be empty outside large countries
	Text    string // condition.text, Chinese when lang=zh
	Code    int    // condition.code, for the short-name table
	TempC   float64
	MaxC    float64
	MinC    float64
	Fetched time.Time // when this data left the API
}

// Short returns the condition as at most four Han characters, or the empty
// string when there is nothing sensible to draw.
//
// The API answers in Chinese (lang=zh), but its wording is a whole sentence for
// some codes ("Patchy rain nearby" → "局部有雨"). The board's second header
// line has room for four, so anything longer falls back to the built-in table,
// keyed by the machine-readable condition code.
func (r Report) Short() string {
	if r.Code == 0 {
		return ""
	}
	if n := len([]rune(r.Text)); n > 0 && n <= 4 {
		return r.Text
	}
	if s, ok := shortNames[r.Code]; ok {
		return s
	}
	// Unknown long text: keep it recognisable rather than inventing a word.
	runes := []rune(r.Text)
	if len(runes) > 4 {
		runes = runes[:4]
	}
	return string(runes)
}

// Stale reports whether the data is old enough that the settings page should
// say so. The frame never hides weather just because it is old: an offline Mac
// still has yesterday's temperature, which beats a blank slot.
func (r Report) Stale(now time.Time, max time.Duration) bool {
	if r.Fetched.IsZero() {
		return true
	}
	return now.Sub(r.Fetched) > max
}
