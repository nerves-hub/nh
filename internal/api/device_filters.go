/*
Copyright © 2026 NervesHub

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in
all copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN
THE SOFTWARE.
*/

package api

import (
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// The device list's filters and sort fields, as the server applies them to
// GET /orgs/{org}/products/{product}/devices. They come from
// NervesHub.Devices.DeviceFiltering (lib/nerves_hub/devices/device_filtering.ex
// in nerves_hub_web).
//
// The server is unforgiving here. A filter key it doesn't know is dropped by
// a catch-all clause, so the list silently comes back unfiltered, and a key or
// sort field that isn't already an atom on the server is a 500, because the
// API controller converts both with String.to_existing_atom/1. Checking them
// here turns both into an error that says what would work.

// deviceFilter describes one filter key the API applies.
type deviceFilter struct {
	// values lists the accepted values; nil accepts anything.
	values []string
	// multi filters take several comma-separated choices, matching any.
	multi bool
	// check validates a free-form value; nil accepts anything.
	check func(string) error
}

var deviceFilters = map[string]deviceFilter{
	"advanced_query": {},
	"alarm":          {multi: true},
	"alarm_status":   {multi: true, values: []string{"with", "without"}},
	"connection": {multi: true, values: []string{
		"connected", "disconnected", "not_seen", "offline",
		"not_seen_in_seven_days", "not_seen_in_fourteen_days",
	}},
	"connection_type": {multi: true, values: []string{"cellular", "ethernet", "wifi", "unknown"}},
	"deployment_id":   {check: checkDeploymentID},
	// The server keeps devices that match any value, but only "include",
	// "exclude" and "only" mean anything.
	"display_deleted":            {values: []string{"exclude", "include", "only"}},
	"firmware_validation_status": {multi: true, values: []string{"validated", "not_validated", "unknown"}},
	"firmware_version":           {multi: true},
	// The API treats any value as true, so only "true" is accepted here.
	"has_no_tags":      {values: []string{"true"}},
	"health_status":    {multi: true, values: []string{"healthy", "warning", "unhealthy", "unknown"}},
	"identifier":       {},
	"metrics_key":      {},
	"metrics_operator": {values: []string{"gt", "lt"}},
	"metrics_value":    {check: checkMetricValue},
	"platform":         {},
	"reason":           {values: []string{"unhealthy", "failing", "not_seen"}},
	"search":           {},
	"signer_ca":        {},
	"state":            {values: []string{"attention", "online", "updating", "offline"}},
	"tags":             {check: checkTags},
	"updates": {multi: true, values: []string{
		"enabled", "disabled", "updating", "automatic", "device-managed",
		"penalty-box", "failed-updates",
	}},
}

// deviceSortFields are the columns the device list can be sorted by
// (DeviceFiltering's @sortable_fields).
var deviceSortFields = []string{"identifier", "connection_established_at", "connection_last_seen_at", "tags"}

// DeviceFilterKeys returns the filter keys the device list accepts, sorted.
func DeviceFilterKeys() []string {
	return slices.Sorted(maps.Keys(deviceFilters))
}

// DeviceSortFields returns the columns the device list can be sorted by.
func DeviceSortFields() []string {
	return slices.Clone(deviceSortFields)
}

// ValidateDeviceSort returns an error unless field is a column the device list
// can be sorted by.
func ValidateDeviceSort(field string) error {
	if slices.Contains(deviceSortFields, field) {
		return nil
	}
	return fmt.Errorf("invalid --sort %q: sort by one of %s", field, strings.Join(deviceSortFields, ", "))
}

// PrepareDeviceFilters checks filters against the keys and values the device
// list applies, and returns them ready to send. The metric filter only takes
// effect when its key, operator and value are all present, so a missing
// operator is filled in with the server's default, "gt".
func PrepareDeviceFilters(filters map[string]string) (map[string]string, error) {
	if len(filters) == 0 {
		return filters, nil
	}

	for _, key := range slices.Sorted(maps.Keys(filters)) {
		spec, ok := deviceFilters[key]
		if !ok {
			return nil, unknownFilterError(key)
		}
		if err := spec.validate(key, filters[key]); err != nil {
			return nil, err
		}
	}

	_, hasKey := filters["metrics_key"]
	_, hasValue := filters["metrics_value"]
	_, hasOperator := filters["metrics_operator"]
	if (hasKey || hasValue || hasOperator) && !(hasKey && hasValue) {
		return nil, fmt.Errorf("the metric filter needs both metrics_key and metrics_value (and optionally metrics_operator:gt or lt), e.g. --filter metrics_key:cpu_temp --filter metrics_value:70")
	}

	out := maps.Clone(filters)
	if hasKey && !hasOperator {
		out["metrics_operator"] = "gt"
	}
	return out, nil
}

func (f deviceFilter) validate(key, value string) error {
	if f.check != nil {
		if err := f.check(value); err != nil {
			return fmt.Errorf("invalid --filter %s:%s: %w", key, value, err)
		}
	}
	if f.values == nil {
		if f.multi && strings.Trim(value, ",") == "" {
			return fmt.Errorf("invalid --filter %s:%s: give at least one value", key, value)
		}
		return nil
	}

	choices := []string{value}
	if f.multi {
		// Split as the server does: on commas, dropping empty choices but
		// not trimming spaces, which would then not match.
		choices = slices.DeleteFunc(strings.Split(value, ","), func(s string) bool { return s == "" })
		if len(choices) == 0 {
			// The server would apply no filter at all.
			return fmt.Errorf("invalid --filter %s:%s: give at least one value", key, value)
		}
	}
	for _, choice := range choices {
		if !slices.Contains(f.values, choice) {
			hint := "one of"
			if f.multi {
				hint = "one or more, comma-separated, of"
			}
			return fmt.Errorf("invalid --filter %s:%s: %q is not a %s value; use %s %s", key, value, choice, key, hint, strings.Join(f.values, ", "))
		}
	}
	return nil
}

func unknownFilterError(key string) error {
	msg := fmt.Sprintf("unknown --filter key %q", key)
	if _, ok := deviceFilters[key+"s"]; ok {
		msg += fmt.Sprintf(" (did you mean %q?)", key+"s")
	}
	return fmt.Errorf("%s: the server ignores keys it doesn't know; use one of %s", msg, strings.Join(DeviceFilterKeys(), ", "))
}

func checkDeploymentID(value string) error {
	if id, err := strconv.ParseUint(value, 10, 63); err != nil || id == 0 {
		return fmt.Errorf("want a deployment group's numeric ID")
	}
	return nil
}

// checkTags applies the server's tag parsing (NervesHub.Types.Tag.cast/1):
// comma-separated, trimmed, and no spaces inside a tag. A value it can't cast,
// or one with no tags, filters nothing.
func checkTags(value string) error {
	n := 0
	for _, tag := range strings.Split(value, ",") {
		tag = strings.TrimSpace(tag)
		if tag == "" {
			continue
		}
		if strings.Contains(tag, " ") {
			return fmt.Errorf("tags cannot contain spaces; separate tags with commas")
		}
		n++
	}
	if n == 0 {
		return fmt.Errorf("give at least one tag")
	}
	return nil
}

// plainNumber is a decimal number, as the metric filter compares with. It
// rules out what strconv.ParseFloat also accepts: NaN, Inf, hex and
// underscores.
var plainNumber = regexp.MustCompile(`^-?(\d+\.?\d*|\.\d+)$`)

func checkMetricValue(value string) error {
	if !plainNumber.MatchString(value) {
		return fmt.Errorf("want a number")
	}
	return nil
}
