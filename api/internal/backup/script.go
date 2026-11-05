package backup

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
)

// The restore reads a backup as the text script of `pg_restore --data-only`
// (run offline: the archive in, the script out, no database connection). The
// script is accepted only if it consists of the session settings pg_restore
// writes, comments, psql's \restrict guard and COPY blocks into the business
// and gk_meta schemas; anything else (a function call, DDL, a sequence
// reset this code does not know) is refused. The COPY statements are rebuilt
// from validated identifiers, never executed as written.

// copyBlock is one table's data in the script.
type copyBlock struct {
	Schema  string
	Table   string
	Columns []string
}

// Statement is the COPY statement for the block, from quoted identifiers.
func (b copyBlock) Statement() string {
	cols := make([]string, len(b.Columns))
	for i, c := range b.Columns {
		cols[i] = pgx.Identifier{c}.Sanitize()
	}
	return "COPY " + pgx.Identifier{b.Schema, b.Table}.Sanitize() + " (" + strings.Join(cols, ", ") + ") FROM STDIN"
}

var (
	copyLine    = regexp.MustCompile(`^COPY (business|gk_meta)\.("?)([a-z_][a-z0-9_]*)("?) \((.+)\) FROM stdin;$`)
	columnName  = regexp.MustCompile(`^("?)([a-z_][a-z0-9_]*)("?)$`)
	restrictKey = regexp.MustCompile(`^\\(un)?restrict [A-Za-z0-9]{1,128}$`)
	setLine     = regexp.MustCompile(`^SET ([a-z_]+) = (.{1,40});$`)
	// pg_restore's session settings; none changes what the data means.
	allowedSettings = map[string]bool{
		"statement_timeout": true, "lock_timeout": true, "idle_in_transaction_session_timeout": true,
		"transaction_timeout": true, "client_encoding": true, "standard_conforming_strings": true,
		"check_function_bodies": true, "xmloption": true, "client_min_messages": true, "row_security": true,
		"default_tablespace": true, "default_table_access_method": true,
	}
)

// errScript reports where the script is not what the restore accepts. It
// names a line number only: a line can hold row data.
func errScript(line int, what string) error {
	return fmt.Errorf("backup: data script line %d: %s", line, what)
}

// parseScript reads a data-only script and calls onCopy for every COPY
// block with a reader over the block's raw COPY text rows (each ending in a
// newline, without the terminating `\.`). onCopy may stop reading early; the
// rest of the block is skipped.
func parseScript(r io.Reader, onCopy func(b copyBlock, rows io.Reader) error) error {
	br := bufio.NewReaderSize(r, 64<<10)
	seen := map[string]bool{}
	line := 0
	for {
		raw, err := br.ReadBytes('\n')
		if len(raw) == 0 && err == io.EOF {
			return nil
		}
		if err != nil && err != io.EOF {
			return fmt.Errorf("backup: read data script: %w", err)
		}
		line++
		text := strings.TrimSuffix(string(raw), "\n")
		switch {
		case text == "" || strings.HasPrefix(text, "--"):
			continue
		case restrictKey.MatchString(text):
			continue
		case text == "SELECT pg_catalog.set_config('search_path', '', false);":
			continue
		case strings.HasPrefix(text, "SET "):
			m := setLine.FindStringSubmatch(text)
			if m == nil || !allowedSettings[m[1]] {
				return errScript(line, "unexpected session setting")
			}
			if m[1] == "client_encoding" && m[2] != "'UTF8'" {
				return errScript(line, "the dump is not UTF8")
			}
			continue
		case strings.HasPrefix(text, "COPY "):
		default:
			return errScript(line, "not a data statement")
		}
		m := copyLine.FindStringSubmatch(text)
		if m == nil || (m[2] == `"`) != (m[4] == `"`) {
			return errScript(line, "unexpected COPY statement")
		}
		b := copyBlock{Schema: m[1], Table: m[3]}
		for _, c := range strings.Split(m[5], ", ") {
			cm := columnName.FindStringSubmatch(c)
			if cm == nil || cm[1] != cm[3] {
				return errScript(line, "unexpected COPY column list")
			}
			b.Columns = append(b.Columns, cm[2])
		}
		key := b.Schema + "." + b.Table
		if seen[key] {
			return errScript(line, "table "+key+" appears twice")
		}
		seen[key] = true
		rows := &blockReader{br: br, line: &line}
		if err := onCopy(b, rows); err != nil {
			return err
		}
		if _, err := io.Copy(io.Discard, rows); err != nil {
			return err
		}
	}
}

// blockReader yields a COPY block's rows up to (not including) the `\.`
// line. In COPY text format a row never spans lines (newlines in values are
// escaped), so the terminator is unambiguous.
type blockReader struct {
	br   *bufio.Reader
	line *int
	buf  []byte
	done bool
}

func (b *blockReader) Read(p []byte) (int, error) {
	for len(b.buf) == 0 {
		if b.done {
			return 0, io.EOF
		}
		raw, err := b.br.ReadBytes('\n')
		if err != nil {
			if err == io.EOF {
				return 0, errScript(*b.line+1, "COPY block is not terminated")
			}
			return 0, err
		}
		*b.line++
		if bytes.Equal(raw, []byte("\\.\n")) {
			b.done = true
			return 0, io.EOF
		}
		b.buf = raw
	}
	n := copy(p, b.buf)
	b.buf = b.buf[n:]
	return n, nil
}

// readRows calls row with the fields of every row of a block (text format,
// \N for NULL kept as is; values are not unescaped, so callers read only
// columns that cannot contain escapes: IDs, numbers, fixed words).
func readRows(rows io.Reader, row func(fields []string) error) (int64, error) {
	br := bufio.NewReaderSize(rows, 64<<10)
	var n int64
	for {
		raw, err := br.ReadBytes('\n')
		if len(raw) > 0 {
			n++
			if row != nil {
				if err := row(strings.Split(strings.TrimSuffix(string(raw), "\n"), "\t")); err != nil {
					return n, err
				}
			}
		}
		if err == io.EOF {
			return n, nil
		}
		if err != nil {
			return n, err
		}
	}
}

// MigrationRecord is one row of the backup's migration history.
type MigrationRecord struct {
	Version string
	SHA256  string
}

// Lease is a job lease that was held when the backup was taken.
type Lease struct {
	Job        string
	Worker     string
	Generation int64
}

// dumpSummary is what the restore learns from a backup's data script
// before it touches the target: table row counts, the migration history
// with checksums, and the job leases held at backup time. No row data.
type dumpSummary struct {
	Rows    map[string]int64
	History []MigrationRecord
	Leases  []Lease
}

// summarize reads a data script without loading it.
func summarize(r io.Reader) (dumpSummary, error) {
	s := dumpSummary{Rows: map[string]int64{}}
	err := parseScript(r, func(b copyBlock, rows io.Reader) error {
		var row func([]string) error
		switch {
		case b.Schema == "gk_meta" && b.Table == "schema_migration":
			iv, is := index(b.Columns, "version"), index(b.Columns, "sha256")
			if iv < 0 || is < 0 {
				return errors.New("backup: the dump's migration history has no version or sha256 column")
			}
			row = func(f []string) error {
				if len(f) != len(b.Columns) {
					return errors.New("backup: malformed migration history row")
				}
				s.History = append(s.History, MigrationRecord{Version: f[iv], SHA256: f[is]})
				return nil
			}
		case b.Schema == "gk_meta":
			return fmt.Errorf("backup: the dump holds gk_meta.%s, which the restore does not know", b.Table)
		case b.Table == "job":
			cols := map[string]int{}
			for _, c := range []string{"id", "state", "lease_owner", "generation"} {
				if cols[c] = index(b.Columns, c); cols[c] < 0 {
					return fmt.Errorf("backup: the dump's job table has no %s column", c)
				}
			}
			row = func(f []string) error {
				if len(f) != len(b.Columns) {
					return errors.New("backup: malformed job row")
				}
				if f[cols["state"]] != "running" {
					return nil
				}
				gen, err := strconv.ParseInt(f[cols["generation"]], 10, 64)
				if err != nil {
					return errors.New("backup: malformed job generation")
				}
				s.Leases = append(s.Leases, Lease{Job: f[cols["id"]], Worker: f[cols["lease_owner"]], Generation: gen})
				return nil
			}
		}
		n, err := readRows(rows, row)
		if err != nil {
			return err
		}
		if b.Schema == "business" {
			s.Rows[b.Table] = n
		}
		return nil
	})
	slices.SortFunc(s.History, func(a, b MigrationRecord) int { return strings.Compare(a.Version, b.Version) })
	return s, err
}

func index(cols []string, name string) int {
	for i, c := range cols {
		if c == name {
			return i
		}
	}
	return -1
}
