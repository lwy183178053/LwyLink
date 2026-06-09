package dbutil

import (
	"database/sql"
)

func RequireAffected(res sql.Result, err error) error {
	return RequireAffectedAs(sql.ErrNoRows, res, err)
}

func RequireAffectedAs(notFoundErr error, res sql.Result, err error) error {
	if err != nil {
		return err
	}
	affected, err := res.RowsAffected()
	if err == nil && affected == 0 {
		return notFoundErr
	}
	return err
}
