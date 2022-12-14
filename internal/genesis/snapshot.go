package genesis

import (
	"strings"

	"gitlab.com/accumulatenetwork/accumulate/internal/database/snapshot"
	"gitlab.com/accumulatenetwork/accumulate/internal/errors"
	"gitlab.com/accumulatenetwork/accumulate/internal/logging"
	"gitlab.com/accumulatenetwork/accumulate/internal/routing"
	"gitlab.com/accumulatenetwork/accumulate/pkg/url"
)

type snapshotVisitor struct {
	v         *snapshot.RestoreVisitor
	logger    logging.OptionalLogger
	router    routing.Router
	partition string
	urls      []*url.URL

	accounts     int
	transactions int
}

func (v *snapshotVisitor) VisitSection(s *snapshot.ReaderSection) error {
	v.logger.Info("Section", "module", "restore", "type", s.Type(), "offset", s.Offset(), "size", s.Size())
	switch s.Type() {
	case snapshot.SectionTypeAccounts,
		snapshot.SectionTypeTransactions,
		snapshot.SectionTypeGzTransactions:
		return nil // Ok

	case snapshot.SectionTypeHeader:
		return nil // Ignore extra headers

	default:
		return errors.Format(errors.StatusBadRequest, "unexpected %v section", s.Type())
	}
}

func (v *snapshotVisitor) VisitAccount(acct *snapshot.Account, _ int) error {
	if acct == nil {
		err := v.v.VisitAccount(nil, v.accounts)
		v.accounts = 0
		return errors.Wrap(errors.StatusUnknownError, err)
	}

	partition, err := v.router.RouteAccount(acct.Url)
	if err != nil {
		return errors.Format(errors.StatusInternalError, "route %v: %w", acct.Url, err)
	}

	if !strings.EqualFold(partition, v.partition) {
		return nil
	}

	v.urls = append(v.urls, acct.Url)
	err = v.v.VisitAccount(acct, v.accounts)
	v.accounts++
	return errors.Wrap(errors.StatusUnknownError, err)
}

func (v *snapshotVisitor) VisitTransaction(txn *snapshot.Transaction, _ int) error {
	if txn == nil {
		err := v.v.VisitTransaction(nil, v.transactions)
		v.transactions = 0
		return errors.Wrap(errors.StatusUnknownError, err)
	}

	u := txn.Transaction.Header.Principal
	partition, err := v.router.RouteAccount(u)
	if err != nil {
		return errors.Format(errors.StatusInternalError, "route %v: %w", u, err)
	}

	if !strings.EqualFold(partition, v.partition) {
		return nil
	}

	err = v.v.VisitTransaction(txn, v.transactions)
	v.transactions++
	return errors.Wrap(errors.StatusUnknownError, err)
}
