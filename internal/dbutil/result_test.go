package dbutil

import (
	"database/sql"
	"errors"
	"testing"
)

type fakeResult struct {
	affected int64
	err      error
}

func (r fakeResult) LastInsertId() (int64, error) {
	return 0, nil
}

func (r fakeResult) RowsAffected() (int64, error) {
	return r.affected, r.err
}

func TestRequireAffected(t *testing.T) {
	if err := RequireAffected(nil, errors.New("exec failed")); err == nil {
		t.Fatal("RequireAffected did not return exec error")
	}
	if err := RequireAffected(fakeResult{}, nil); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("RequireAffected zero rows = %v, want sql.ErrNoRows", err)
	}
	if err := RequireAffected(fakeResult{affected: 1}, nil); err != nil {
		t.Fatalf("RequireAffected one row = %v, want nil", err)
	}
	if err := RequireAffected(fakeResult{affected: 1, err: errors.New("rows failed")}, nil); err == nil {
		t.Fatal("RequireAffected did not return RowsAffected error")
	}
}

func TestRequireAffectedAs(t *testing.T) {
	notFound := errors.New("not found")
	if err := RequireAffectedAs(notFound, fakeResult{}, nil); !errors.Is(err, notFound) {
		t.Fatalf("RequireAffectedAs zero rows = %v, want custom not found", err)
	}
}
