package implementation

import (
	"context"
	"errors"
	. "github.com/MahdiPezeshkian/Onyx.Data/db/sql/contract"
	"strings"

	"gorm.io/gorm"
)

// CommandBaseRepository executes raw SQL update commands on a transaction handle. It
// performs no transaction management itself: the supplied *gorm.DB determines whether
// the command joins an existing transaction.
type CommandBaseRepository struct {
	tx *gorm.DB
}

// NewCommandRepository builds a command repository that runs statements on tx.
func NewCommandRepository(tx *gorm.DB) *CommandBaseRepository {
	return &CommandBaseRepository{
		tx: tx,
	}
}

// ApplyUpdateCommand validates and executes the UPDATE statement returned by command
// and reports the number of affected rows. It returns an error if the query is empty,
// fails validation, or the execution fails. The command and its arguments are passed
// to the database as a parameterized statement.
func (c *CommandBaseRepository) ApplyUpdateCommand(
	ctx context.Context,
	command Command,
) (rowsAffected int64, err error) {

	query, args := command.RawUpdateQuery()
	if query == "" {
		err = errors.New("empty command query")
		return
	}

	err = validateUpdateQuery(query)
	if err != nil {
		return
	}

	res := c.tx.
		WithContext(ctx).
		Exec(query, args...)

	if res.Error != nil {
		err = res.Error
		return
	}

	rowsAffected = res.RowsAffected
	return
}

// validateUpdateQuery rejects anything that is not a single UPDATE statement with a
// WHERE clause: the query must start with "update", must not contain the forbidden
// keywords (delete, truncate, drop, alter, create), and must have its "where" after
// its "set". Keyword matching is textual, so it is a guardrail rather than a full SQL
// parser.
func validateUpdateQuery(query string) error {
	q := strings.TrimSpace(strings.ToLower(query))

	if !strings.HasPrefix(q, "update ") {
		return errors.New("only UPDATE queries are allowed")
	}

	forbidden := []string{
		" delete ",
		" truncate ",
		" drop ",
		" alter ",
		" create ",
	}

	for _, f := range forbidden {
		if strings.Contains(q, f) {
			return errors.New("forbidden sql keyword detected")
		}
	}

	// WHERE must exist and must be after SET
	// WHERE must exist and must be after SET
	whereIdx := strings.Index(q, "where ")
	setIdx := strings.Index(q, "set ")

	if setIdx == -1 || whereIdx == -1 || whereIdx < setIdx {
		return errors.New("UPDATE query must contain a WHERE clause")
	}

	return nil
}
