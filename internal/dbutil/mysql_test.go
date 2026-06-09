package dbutil

import (
	"errors"
	"fmt"
	"testing"

	"github.com/go-sql-driver/mysql"
)

func TestDuplicateEntry(t *testing.T) {
	duplicateErr := &mysql.MySQLError{Number: 1062}
	if !DuplicateEntry(duplicateErr) {
		t.Fatal("DuplicateEntry did not match MySQL 1062")
	}
	if !DuplicateEntry(fmt.Errorf("wrapped: %w", duplicateErr)) {
		t.Fatal("DuplicateEntry did not match wrapped MySQL 1062")
	}
	if DuplicateEntry(&mysql.MySQLError{Number: 1048}) {
		t.Fatal("DuplicateEntry matched a different MySQL error")
	}
	if DuplicateEntry(errors.New("plain error")) {
		t.Fatal("DuplicateEntry matched a non-MySQL error")
	}
}
