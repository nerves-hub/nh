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

package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"

	"github.com/nerves-hub/nh/internal/api"
	"github.com/nerves-hub/nh/internal/config"
)

// querySupportFile records, per API URI, when a server last passed the
// advanced query check, so `nh device list --query` doesn't repeat it on
// every call.
const querySupportFile = "advanced-query-support.json"

// querySupportTTL is how long a passed check is trusted. Only passes are
// recorded: a server that fails is checked again next time, so an upgrade is
// picked up at once.
const querySupportTTL = 24 * time.Hour

// requireAdvancedQuery returns an error unless the server applies
// filters[advanced_query]. A server that doesn't would answer with every
// device, as if they all matched.
func requireAdvancedQuery(ctx context.Context, cfg *config.Config, client *api.Client, org, product string) error {
	path := filepath.Join(cfg.DataDir, querySupportFile)
	checked := readQuerySupport(path)
	if at, ok := checked[cfg.URI]; ok && time.Since(at) < querySupportTTL {
		return nil
	}

	supported, err := client.SupportsAdvancedQuery(ctx, org, product)
	if err != nil {
		return err
	}
	if !supported {
		return errors.New("this NervesHub server doesn't support advanced queries (it needs v2.5.0 or later), so it would ignore the query and list every device")
	}

	// Best effort: failing to record the pass only means checking again.
	checked[cfg.URI] = time.Now()
	writeQuerySupport(cfg.DataDir, path, checked)
	return nil
}

// readQuerySupport returns the recorded passes, or an empty map when there
// are none or the file can't be read.
func readQuerySupport(path string) map[string]time.Time {
	checked := map[string]time.Time{}
	data, err := os.ReadFile(path)
	if err != nil {
		return checked
	}
	if err := json.Unmarshal(data, &checked); err != nil {
		return map[string]time.Time{}
	}
	return checked
}

func writeQuerySupport(dataDir, path string, checked map[string]time.Time) {
	data, err := json.MarshalIndent(checked, "", "  ")
	if err != nil || config.EnsureDataDir(dataDir) != nil {
		return
	}
	_ = os.WriteFile(path, append(data, '\n'), 0o600)
}
