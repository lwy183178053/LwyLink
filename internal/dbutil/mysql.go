package dbutil

import (
	"errors"

	"github.com/go-sql-driver/mysql"
)

func DuplicateEntry(err error) bool {
	var mysqlErr *mysql.MySQLError
	return errors.As(err, &mysqlErr) && mysqlErr.Number == 1062
}
