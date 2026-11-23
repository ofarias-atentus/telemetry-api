package api

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type execCall struct {
	sql  string
	args []any
}

type copyCall struct {
	table   pgx.Identifier
	columns []string
	rows    [][]any
}

// fakeDB registra las llamadas y devuelve los errores configurados.
type fakeDB struct {
	execCalls []execCall
	copyCalls []copyCall

	// execErr devuelve el error para la fila cuyo primer argumento (device_id) coincida.
	execErr map[string]error

	copyN   int64
	copyErr error
}

func (f *fakeDB) Exec(_ context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	f.execCalls = append(f.execCalls, execCall{sql: sql, args: args})
	if len(args) > 0 {
		if id, ok := args[0].(string); ok {
			if err := f.execErr[id]; err != nil {
				return pgconn.CommandTag{}, err
			}
		}
	}
	return pgconn.CommandTag{}, nil
}

func (f *fakeDB) CopyFrom(
	_ context.Context,
	tableName pgx.Identifier,
	columnNames []string,
	rowSrc pgx.CopyFromSource,
) (int64, error) {
	call := copyCall{table: tableName, columns: columnNames}
	for rowSrc.Next() {
		vals, err := rowSrc.Values()
		if err != nil {
			return 0, err
		}
		call.rows = append(call.rows, vals)
	}
	f.copyCalls = append(f.copyCalls, call)
	return f.copyN, f.copyErr
}
