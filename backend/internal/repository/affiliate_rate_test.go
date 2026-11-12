package repository

import (
	"context"
	"errors"
	"regexp"
	"testing"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/DATA-DOG/go-sqlmock"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/stretchr/testify/require"
)

func TestAffiliateRateOnlySelectsCurrentUser(t *testing.T) {
	for _, tc := range []struct {
		name string
		rows *sqlmock.Rows
		want *float64
		fail bool
	}{
		{"missing", sqlmock.NewRows([]string{"rate"}), nil, false},
		{"null", sqlmock.NewRows([]string{"rate"}).AddRow(nil), nil, false},
		{"zero", sqlmock.NewRows([]string{"rate"}).AddRow(0), floatPointerForRate(0), false},
		{"custom", sqlmock.NewRows([]string{"rate"}).AddRow(12.5), floatPointerForRate(12.5), false},
		{"failure", nil, nil, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			require.NoError(t, err)
			client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, db)))
			t.Cleanup(func() { _ = client.Close() })
			expect := mock.ExpectQuery(regexp.QuoteMeta("SELECT aff_rebate_rate_percent::double precision FROM user_affiliates WHERE user_id = $1")).WithArgs(int64(42))
			if tc.fail {
				expect.WillReturnError(errors.New("unavailable"))
			} else {
				expect.WillReturnRows(tc.rows)
			}
			got, err := NewAffiliateRepository(client, db).GetUserRebateRate(context.Background(), 42)
			if tc.fail {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				require.Equal(t, tc.want, got)
			}
			// Any initialization, thaw, ledger query, transaction, or write is unexpected SQL.
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func floatPointerForRate(v float64) *float64 { return &v }
