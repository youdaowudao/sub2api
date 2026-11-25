//go:build unit

package repository

import (
	"context"
	"database/sql/driver"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/DATA-DOG/go-sqlmock"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	dbaccount "github.com/Wei-Shaw/sub2api/ent/account"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestAccountRepository_GetAccountForTokenVersion(t *testing.T) {
	for _, scoped := range []bool{false, true} {
		name := "base"
		if scoped {
			name = "transaction"
		}
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			baseDB, baseMock, err := sqlmock.New()
			require.NoError(t, err)
			base := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, baseDB)))
			defer func() { _ = base.Close() }()
			repo := newAccountRepositoryWithSQL(base, baseDB, nil)
			mock := baseMock
			if scoped {
				txDB, txMock, err := sqlmock.New()
				require.NoError(t, err)
				txClient := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, txDB)))
				defer func() { _ = txClient.Close() }()
				txMock.ExpectBegin()
				tx, err := txClient.Tx(ctx)
				require.NoError(t, err)
				ctx = dbent.NewTxContext(ctx, tx)
				mock = txMock
				defer func() {
					txMock.ExpectRollback()
					require.NoError(t, tx.Rollback())
					require.NoError(t, txMock.ExpectationsWereMet())
				}()
			}

			row := make([]driver.Value, len(dbaccount.Columns))
			for i, col := range dbaccount.Columns {
				switch col {
				case dbaccount.FieldID:
					row[i] = int64(25)
				case dbaccount.FieldName:
					row[i] = "test"
				case dbaccount.FieldCreatedAt, dbaccount.FieldUpdatedAt:
					row[i] = time.Now()
				case dbaccount.FieldPlatform:
					row[i] = service.PlatformOpenAI
				case dbaccount.FieldType:
					row[i] = service.AccountTypeOAuth
				case dbaccount.FieldCredentials:
					row[i] = []byte(`{"_token_version":27}`)
				case dbaccount.FieldProxyID:
					row[i] = int64(26)
				}
			}
			mock.ExpectQuery(`(?s)SELECT .* FROM "accounts" WHERE .*`).WithArgs(int64(25)).
				WillReturnRows(sqlmock.NewRows(dbaccount.Columns).AddRow(row...))

			got, err := repo.GetAccountForTokenVersion(ctx, 25)
			require.NoError(t, err)
			require.Equal(t, service.PlatformOpenAI, got.Platform)
			require.Equal(t, int64(27), got.GetCredentialAsInt64("_token_version"))
			require.NoError(t, mock.ExpectationsWereMet())
			require.NoError(t, baseMock.ExpectationsWereMet())
		})
	}
}
