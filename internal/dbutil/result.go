package dbutil

import (
	"database/sql"
)

func RequireAffected(res sql.Result, err error) error {
	if err != nil {
		return err
	}
	affected, err := res.RowsAffected()
	if err == nil && affected == 0 {
		return sql.ErrNoRows
	}
	return err
}
