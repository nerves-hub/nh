package api

import (
	"encoding/json"
	"testing"
	"time"
)

func TestTimestampUnmarshal(t *testing.T) {
	cases := map[string]struct {
		in   string
		want time.Time
	}{
		"zoneless T (orgs)":         {`"2024-05-21T11:06:42"`, time.Date(2024, 5, 21, 11, 6, 42, 0, time.UTC)},
		"rfc3339 Z":                 {`"2026-01-02T15:04:05Z"`, time.Date(2026, 1, 2, 15, 4, 5, 0, time.UTC)},
		"zoneless fractional":       {`"2024-05-21T11:06:42.5"`, time.Date(2024, 5, 21, 11, 6, 42, 500000000, time.UTC)},
		"space sep micros Z (devs)": {`"2026-06-03 03:37:01.833917Z"`, time.Date(2026, 6, 3, 3, 37, 1, 833917000, time.UTC)},
		"space sep zoneless":        {`"2026-06-03 03:37:01"`, time.Date(2026, 6, 3, 3, 37, 1, 0, time.UTC)},
		"offset zone":               {`"2026-01-02T15:04:05+02:00"`, time.Date(2026, 1, 2, 15, 4, 5, 0, time.FixedZone("", 2*60*60))},
		"null":                      {`null`, time.Time{}},
		"empty":                     {`""`, time.Time{}},
		"never sentinel":            {`"never"`, time.Time{}},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			var ts Timestamp
			if err := json.Unmarshal([]byte(tc.in), &ts); err != nil {
				t.Fatalf("unmarshal %s: %v", tc.in, err)
			}
			if !ts.Time.Equal(tc.want) {
				t.Errorf("got %v, want %v", ts.Time, tc.want)
			}
		})
	}
}

func TestTimestampUnmarshalInvalid(t *testing.T) {
	var ts Timestamp
	if err := json.Unmarshal([]byte(`"not-a-time"`), &ts); err == nil {
		t.Error("expected error for invalid timestamp")
	}
}
