package dolthubauth

import (
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

func TestIsUniqueViolation(t *testing.T) {
	t.Run("postgres unique violation", func(t *testing.T) {
		err := &pgconn.PgError{Code: "23505"}
		if !isUniqueViolation(err) {
			t.Fatal("expected unique violation to be detected")
		}
	})

	t.Run("wrapped postgres unique violation", func(t *testing.T) {
		err := errors.New("outer")
		wrapped := errors.Join(err, &pgconn.PgError{Code: "23505"})
		if !isUniqueViolation(wrapped) {
			t.Fatal("expected wrapped unique violation to be detected")
		}
	})

	t.Run("non unique postgres error", func(t *testing.T) {
		err := &pgconn.PgError{Code: "23503"}
		if isUniqueViolation(err) {
			t.Fatal("did not expect foreign-key violation to match")
		}
	})

	t.Run("non postgres error", func(t *testing.T) {
		if isUniqueViolation(errors.New("duplicate")) {
			t.Fatal("did not expect plain string error to match")
		}
	})
}
