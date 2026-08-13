//go:build ignore

// Compiles the IEEE MA-L registry into the table pkg/oui embeds. The raw CSV is
// 3.6MB of mostly postal addresses; we keep the prefix and the name.
//
// Usage: go run pkg/oui/gen/main.go [-url URL] -out pkg/oui/oui.tsv.gz
package main

import (
	"compress/gzip"
	"encoding/csv"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"
)

const defaultURL = "https://standards-oui.ieee.org/oui/oui.csv"

func main() {
	src := flag.String("src", defaultURL, "IEEE MA-L registry CSV: a URL or a local path")
	out := flag.String("out", "pkg/oui/oui.tsv.gz", "output path")
	flag.Parse()

	rows, err := load(*src)
	if err != nil {
		log.Fatal(err)
	}
	if len(rows) < 30000 {
		// The registry has ~40k assignments. A short read means a truncated
		// download or a changed format, not a shrinking registry.
		log.Fatalf("only %d assignments; refusing to write a truncated table", len(rows))
	}
	if err := write(*out, rows); err != nil {
		log.Fatal(err)
	}
	fmt.Printf("wrote %s (%d assignments)\n", *out, len(rows))
}

type row struct{ prefix, name string }

func load(src string) ([]row, error) {
	if !strings.HasPrefix(src, "http://") && !strings.HasPrefix(src, "https://") {
		f, err := os.Open(src)
		if err != nil {
			return nil, err
		}
		defer f.Close()
		return parse(f)
	}

	req, err := http.NewRequest(http.MethodGet, src, nil)
	if err != nil {
		return nil, err
	}
	// IEEE answers 418 to the default Go user-agent.
	req.Header.Set("User-Agent", "nasnet-panel-oui-gen/1.0")

	resp, err := (&http.Client{Timeout: 2 * time.Minute}).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s: %s", src, resp.Status)
	}
	return parse(resp.Body)
}

// parse keeps MA-L only: MA-M and MA-S are sub-allocations inside a /24, so a
// 24-bit lookup would return the block holder rather than the real vendor.
func parse(r io.Reader) ([]row, error) {
	cr := csv.NewReader(r)
	cr.FieldsPerRecord = -1
	recs, err := cr.ReadAll()
	if err != nil {
		return nil, err
	}

	seen := map[string]bool{}
	var out []row
	for _, rec := range recs {
		if len(rec) < 3 || rec[0] != "MA-L" {
			continue
		}
		prefix := strings.ToLower(strings.TrimSpace(rec[1]))
		name := clean(rec[2])
		if len(prefix) != 6 || name == "" || seen[prefix] {
			continue
		}
		seen[prefix] = true
		out = append(out, row{prefix, name})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].prefix < out[j].prefix })
	return out, nil
}

// clean strips what would break the TSV or bloat the table. Names are shown in
// a device list, so a 100-char legal entity name is noise.
func clean(s string) string {
	s = strings.Map(func(r rune) rune {
		if r == '\t' || r == '\n' || r == '\r' {
			return ' '
		}
		return r
	}, s)
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > 64 {
		s = strings.TrimSpace(s[:64])
	}
	return s
}

func write(path string, rows []row) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	zw, _ := gzip.NewWriterLevel(f, gzip.BestCompression)
	for _, r := range rows {
		if _, err := fmt.Fprintf(zw, "%s\t%s\n", r.prefix, r.name); err != nil {
			return err
		}
	}
	if err := zw.Close(); err != nil {
		return err
	}
	return f.Close()
}
