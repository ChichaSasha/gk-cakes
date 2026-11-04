package main

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

// loginRow answers the login query: a login holding groups, or an error.
type loginRow struct {
	groups []string
	err    error
}

func (r loginRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	*dest[0].(*string), *dest[1].(*string), *dest[2].(*bool) = "w", "w", false
	*dest[3].(*[]string), *dest[4].(*[]string) = r.groups, r.groups
	return nil
}

type loginDB struct{ row loginRow }

func (d loginDB) QueryRow(context.Context, string, ...any) pgx.Row { return d.row }

func TestVerifyLoginExitCodes(t *testing.T) {
	for name, c := range map[string]struct {
		row  loginRow
		code int
	}{
		"worker login":   {loginRow{groups: []string{"gk_worker"}}, 0},
		"api login":      {loginRow{groups: []string{"gk_api"}}, 2},
		"unreachable db": {loginRow{err: errors.New("connection refused")}, 1},
	} {
		code, err := verifyLogin(context.Background(), loginDB{c.row}, 50*time.Millisecond, time.Millisecond)
		if code != c.code || (code == 0) != (err == nil) {
			t.Errorf("%s: exit %d, %v; want exit %d", name, code, err, c.code)
		}
	}
}
